package sap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/oauthclient"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// setupOAuthOrg wires a managed account did:plc:testorg with an OAuth session
// pointing at srvURL.
func setupOAuthOrg(t *testing.T, srvURL string) (*gorm.DB, *oauthclient.App) {
	t.Helper()
	db := openTestDB(t)
	store, err := oauthclient.NewGormStore(db)
	require.NoError(t, err)
	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	oauthApp := oauthclient.NewApp(&cfg, store)
	require.NoError(t, store.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:  "did:plc:testorg",
		SessionID:   "sess1",
		HostURL:     srvURL,
		AccessToken: testJWT(t),
	}))
	require.NoError(t, db.Create(&managedOrg{DID: "did:plc:testorg", SessionID: "sess1"}).Error)
	return db, oauthApp
}

// TestRegistrar_RegistersAndRenews checks that a tracked space with no current
// registration is registered, its expiry recorded, and that a fresh
// registration is not re-registered on the next sweep.
func TestRegistrar_RegistersAndRenews(t *testing.T) {
	t.Parallel()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/xrpc/network.habitat.space.registerNotify", r.URL.Path)
		atomic.AddInt32(&calls, 1)
		var in habitat.NetworkHabitatSpaceRegisterNotifyInput
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		require.Equal(t, "https://sap.example", in.Endpoint)
		require.Empty(t, in.Repo, "sap registers for the whole space")
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceRegisterNotifyOutput{
			ExpiresAt: time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		})
	}))
	t.Cleanup(srv.Close)

	db, oauthApp := setupOAuthOrg(t, srv.URL)
	space := habitat_syntax.SpaceURI("ats://did:plc:testorg/network.habitat.space/s1")
	require.NoError(t, db.Create(&managedSpace{Space: space, DID: "did:plc:testorg"}).Error)

	reg := newRegistrar(db, oauthApp, "https://sap.example")
	reg.registerDue(t.Context())

	require.Equal(t, int32(1), atomic.LoadInt32(&calls))
	var nr notifyRegistration
	require.NoError(t, db.First(&nr, "space = ?", space).Error)
	require.True(t, nr.ExpiresAt.After(time.Now()))
	require.Equal(t, "https://sap.example", nr.Endpoint)

	// A registration that is still fresh must not be re-registered.
	reg.registerDue(t.Context())
	require.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

// TestRegistrar_DueSpaces checks the selection of spaces that need
// (re-)registration: missing or close to expiry, but not those with plenty of
// lifetime left.
func TestRegistrar_DueSpaces(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	reg := newRegistrar(db, nil, "https://sap.example")

	missing := habitat_syntax.SpaceURI("ats://did:plc:o/network.habitat.space/missing")
	expiring := habitat_syntax.SpaceURI("ats://did:plc:o/network.habitat.space/expiring")
	fresh := habitat_syntax.SpaceURI("ats://did:plc:o/network.habitat.space/fresh")
	for _, s := range []habitat_syntax.SpaceURI{missing, expiring, fresh} {
		require.NoError(t, db.Create(&managedSpace{Space: s, DID: "did:plc:o"}).Error)
	}
	require.NoError(t, db.Create(&notifyRegistration{
		Space: expiring, ExpiresAt: time.Now().Add(1 * time.Hour),
	}).Error)
	require.NoError(t, db.Create(&notifyRegistration{
		Space: fresh, ExpiresAt: time.Now().Add(48 * time.Hour),
	}).Error)

	due, err := reg.dueSpaces(t.Context())
	require.NoError(t, err)
	require.ElementsMatch(t, []habitat_syntax.SpaceURI{missing, expiring}, due)
}

// TestRegistrar_DedupesSpaceAcrossAccounts checks that a space tracked by
// several managed accounts is registered once, not per account.
func TestRegistrar_DedupesSpaceAcrossAccounts(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	reg := newRegistrar(db, nil, "https://sap.example")

	space := habitat_syntax.SpaceURI("ats://did:plc:o/network.habitat.space/shared")
	require.NoError(t, db.Create(&managedSpace{Space: space, DID: "did:plc:a"}).Error)
	require.NoError(t, db.Create(&managedSpace{Space: space, DID: "did:plc:b"}).Error)

	due, err := reg.dueSpaces(t.Context())
	require.NoError(t, err)
	require.Equal(t, []habitat_syntax.SpaceURI{space}, due)
}
