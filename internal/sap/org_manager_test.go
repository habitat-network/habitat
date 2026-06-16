package sap

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOrgManager_AddOrg(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, autoMigrate(db))
	pearServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(`{"access_token":"token"}`))
			require.NoError(t, err)
		}
	}))
	o := newOrgManager(
		db,
		"sap.domain",
		nil,
		pdsclient.NewDummyDirectory(
			"https://pds.example.com",
			pdsclient.WithHabitatService(pearServer.URL),
		),
	)
	redirectURL, err := o.AddOrg(t.Context(), "example.handle.com")
	require.NoError(t, err)
	parsedURL, err := url.Parse(redirectURL)
	require.NoError(t, err)
	state := parsedURL.Query().Get("state")

	addedOrg, err := o.completeAuth(t.Context(), "code", state)
	require.NoError(t, err)

	require.Equal(t, addedOrg.AccessToken, "token")

	dids, err := o.ListOrgs(t.Context())
	require.NoError(t, err)
	require.Equal(t, []syntax.DID{"did:web:example.did.com"}, dids)
}

func TestOrgManager_GetClient(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, autoMigrate(db))
	o := newOrgManager(db, "", nil, pdsclient.NewDummyDirectory("https://pds.example.com"))
	pearServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dump, err := httputil.DumpRequest(r, true)
		require.NoError(t, err)
		t.Log(string(dump))
		switch r.URL.Path {
		case "/oauth/token":
			require.Equal(t, "refresh_token", r.FormValue("refresh_token"))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(`{"access_token":"refreshed_token"}`))
			require.NoError(t, err)
		case "/xrpc/test.endpoint":
			require.Equal(t, "Bearer refreshed_token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		}
	}))

	db.Save(&managedOrg{
		DID:          "did:plc:testorg",
		Host:         pearServer.URL,
		AccessToken:  "expired_token",
		RefreshToken: "refresh_token",
		ExpiresAt:    time.Now(),
	})

	cl := o.GetClient(t.Context(), "did:plc:testorg")

	resp, err := cl.Get(pearServer.URL + "/xrpc/test.endpoint")
	defer func() { _ = resp.Body.Close() }()
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var updatedOrg managedOrg
	require.NoError(t, db.First(&updatedOrg).Error)

	require.Equal(t, "refreshed_token", updatedOrg.AccessToken)
}
