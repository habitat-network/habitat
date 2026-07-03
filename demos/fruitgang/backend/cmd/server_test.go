package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	habitatapi "github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/sap"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// orgBackend fakes the subset of a Habitat pear instance that the fruitgang
// backend calls into during org onboarding: token exchange plus the
// createSpace/writeTuple/listSpaces XRPC endpoints.
type orgBackend struct {
	*httptest.Server
	createSpaceFail atomic.Bool
	writeTupleInput atomic.Pointer[habitatapi.NetworkHabitatRelationshipWriteTupleInput]
}

func newOrgBackend(t *testing.T) *orgBackend {
	t.Helper()
	ob := &orgBackend{}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, "abc", r.PostForm.Get("code"))
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]string{
			"access_token":  newJwtToken(t, time.Now().Add(time.Hour)),
			"refresh_token": "refresh-token",
		}))
	})

	mux.HandleFunc("POST /xrpc/network.habitat.space.createSpace", func(w http.ResponseWriter, r *http.Request) {
		if ob.createSpaceFail.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(habitatapi.NetworkHabitatSpaceCreateSpaceOutput{
			Uri: "ats://did:web:acme.example/network.habitat.group/fruitgang",
		}))
	})

	mux.HandleFunc("POST /xrpc/network.habitat.relationship.writeTuple", func(w http.ResponseWriter, r *http.Request) {
		var input habitatapi.NetworkHabitatRelationshipWriteTupleInput
		require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
		ob.writeTupleInput.Store(&input)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(habitatapi.NetworkHabitatRelationshipWriteTupleOutput{
			Uri: "ats://did:web:acme.example/network.habitat.relationship.tuple/1",
		}))
	})

	// Stubbed so sap's background crawler (spawned as a side effect of
	// AddManagedOrg) doesn't log spurious errors during these tests.
	mux.HandleFunc("GET /xrpc/network.habitat.space.listSpaces", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(habitatapi.NetworkHabitatSpaceListSpacesOutput{}))
	})

	ob.Server = httptest.NewServer(mux)
	t.Cleanup(ob.Server.Close)
	return ob
}

func newJwtToken(t *testing.T, exp time.Time) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	token, err := jwt.NewWithClaims(
		jwt.SigningMethodPS256,
		jwt.MapClaims{"exp": exp.Unix()},
	).SignedString(key)
	require.NoError(t, err)
	return token
}

func newTestFruitgangServer(t *testing.T, orgBackendURL string) *fruitgangServer {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/fruitgang.db"), &gorm.Config{})
	require.NoError(t, err)

	oauthStore, err := oauthclient.NewGormStore(db)
	require.NoError(t, err)

	cfg := oauth.NewPublicConfig(
		"https://fruitgang.example/client-metadata.json",
		"https://fruitgang.example/oauth-callback",
		[]string{"org:*"},
	)
	dir := pdsclient.NewDummyDirectory("unused-pds", pdsclient.WithHabitatService(orgBackendURL))
	oauthApp := oauthclient.NewApp(&cfg, oauthStore, oauthclient.WithDirectory(dir))

	sapInstance, err := sap.NewSap(sap.SapConfig{DB: db, OAuthClient: oauthApp})
	require.NoError(t, err)

	return newFruitgangServer(sapInstance, oauthApp, "https://fruitgang.example")
}

// connectOrg drives handleAddOrg + handleOAuthCallback for the given DID,
// mirroring what the browser does: POST /add-org to get a redirect URL, then
// GET /oauth-callback with the state/code the org's auth server would send
// back. Returns the callback response so the test can assert on it.
func connectOrg(t *testing.T, s *fruitgangServer, did string) *http.Response {
	t.Helper()

	addOrgBody, err := json.Marshal(map[string]string{"handle": did})
	require.NoError(t, err)
	addOrgReq := httptest.NewRequest(http.MethodPost, "/add-org", bytes.NewReader(addOrgBody))
	addOrgW := httptest.NewRecorder()
	s.handleAddOrg(addOrgW, addOrgReq)
	require.Equal(t, http.StatusOK, addOrgW.Code)

	var addOrgResp struct {
		RedirectURL string `json:"redirect_url"`
	}
	require.NoError(t, json.NewDecoder(addOrgW.Body).Decode(&addOrgResp))

	parsed, err := url.Parse(addOrgResp.RedirectURL)
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	require.NotEmpty(t, state, "expected a fresh OAuth redirect with a state param, got %q", addOrgResp.RedirectURL)

	callbackReq := httptest.NewRequest(
		http.MethodGet,
		"/oauth-callback?state="+state+"&code=abc",
		nil,
	)
	callbackW := httptest.NewRecorder()
	s.handleOAuthCallback(callbackW, callbackReq)
	return callbackW.Result()
}

func TestHandleOAuthCallback_FirstConnection_Succeeds(t *testing.T) {
	ob := newOrgBackend(t)
	s := newTestFruitgangServer(t, ob.URL)
	const did = "did:web:acme.example"

	resp := connectOrg(t, s, did)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)

	_, err := s.sap.GetClient(t.Context(), syntax.DID(did))
	require.NoError(t, err, "sap should manage the org once connection succeeds")

	input := ob.writeTupleInput.Load()
	require.NotNil(t, input, "expected a single writeTuple call granting org members access")
	require.Equal(t, "writer", input.Relation)
	require.Equal(t, "ats://did:web:acme.example/network.habitat.group/fruitgang", input.Object.Space)
	subject, ok := input.Subject.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "network.habitat.relationship.defs#spaceRoleSubject", subject["$type"])
	require.Equal(t, "ats://did:web:acme.example/network.habitat.organization/self", subject["space"])
	require.Equal(t, "reader", subject["role"])
}

func TestHandleOAuthCallback_SpaceCreationFails_OrgNotMarkedManaged(t *testing.T) {
	ob := newOrgBackend(t)
	s := newTestFruitgangServer(t, ob.URL)
	const did = "did:web:acme.example"

	ob.createSpaceFail.Store(true)
	resp := connectOrg(t, s, did)
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	// sap must not think the org is connected: a partial failure here should
	// leave nothing for /getSpaceURI to (incorrectly) treat as "connected",
	// so the admin can just retry the OAuth flow from scratch.
	_, err := s.sap.GetClient(t.Context(), syntax.DID(did))
	require.Error(t, err, "sap should not manage the org after a failed connection attempt")

	ob.createSpaceFail.Store(false)
	resp = connectOrg(t, s, did)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)

	_, err = s.sap.GetClient(t.Context(), syntax.DID(did))
	require.NoError(t, err, "retry should succeed and mark the org managed")
}
