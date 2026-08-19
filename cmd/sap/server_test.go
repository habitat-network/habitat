package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/pkg/oauthclient"
	"github.com/habitat-network/habitat/pkg/sap"
	"github.com/stretchr/testify/require"
)

// newTestServer wires up a sap server with a fresh in-memory-backed OAuth
// client app, suitable for exercising handleAddOrg/handleOAuthCallback
// without any network access.
func newTestServer(t *testing.T) *server {
	t.Helper()

	db := testutil.NewDB(t)

	store, err := oauthclient.NewGormStore(db)
	require.NoError(t, err)

	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	oauthApp := oauth.NewClientApp(&cfg, store)

	s, err := sap.New(sap.Config{DB: db, OAuthClient: oauthApp})
	require.NoError(t, err)

	return NewSapServer(s, oauthApp)
}

func TestHandleAddOrgWithoutReturnToUnaffected(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	body, err := json.Marshal(map[string]string{"handle": "not a valid handle!!"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/org/add", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleAddOrg(w, req)

	// An unresolvable/invalid handle fails fast inside StartAuthFlow (no
	// network call), same as before this change.
	require.Equal(t, http.StatusInternalServerError, w.Code)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.Empty(t, srv.pendingReturnTo)
}

func TestHandleAddOrgStoresReturnToForResolvedDID(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	const testDID = syntax.DID("did:plc:testreturnto")
	const testHandle = syntax.Handle("alice.test")

	mockDir := identity.NewMockDirectory()
	// No Services declared, so PDSEndpoint() is empty and StartAuthFlow
	// fails fast after resolving identity, without any network access.
	mockDir.Insert(identity.Identity{
		DID:    testDID,
		Handle: testHandle,
	})
	srv.oauthClient.Dir = mockDir

	body, err := json.Marshal(map[string]string{
		"handle":    string(testHandle),
		"return_to": "https://app.example.com/callback",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/org/add", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleAddOrg(w, req)

	// StartAuthFlow itself still fails (no PDS host), but our own
	// resolution should have already stored the pending return_to.
	require.Equal(t, http.StatusInternalServerError, w.Code)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.Equal(t, "https://app.example.com/callback", srv.pendingReturnTo[testDID.String()])
}

func TestRedirectToReturnToRedirectsAndClearsPending(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	const testDID = "did:plc:testreturnto"

	srv.mu.Lock()
	srv.pendingReturnTo[testDID] = "https://app.example.com/callback"
	srv.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/oauth-callback", nil)
	w := httptest.NewRecorder()

	handled := srv.redirectToReturnTo(w, req, testDID)
	require.True(t, handled)
	require.Equal(t, http.StatusSeeOther, w.Code)
	require.Equal(
		t,
		"https://app.example.com/callback?did=did%3Aplc%3Atestreturnto",
		w.Header().Get("Location"),
	)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.Empty(t, srv.pendingReturnTo)
}

func TestHandleListOrgsIncludesSessionIDPerEntry(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	const testDID = syntax.DID("did:plc:testlistorgs")
	const testSessionID = "session-abc"
	require.NoError(t, srv.sap.AddSession(t.Context(), testDID, testSessionID))

	req := httptest.NewRequest(http.MethodGet, "/org/list", nil)
	w := httptest.NewRecorder()

	srv.handleListOrgs(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Orgs []orgSession `json:"orgs"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, []orgSession{{DID: testDID, SessionID: testSessionID}}, body.Orgs)
}

func TestRedirectToReturnToNoPendingFallsBackToFalse(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/oauth-callback", nil)
	w := httptest.NewRecorder()

	handled := srv.redirectToReturnTo(w, req, "did:plc:unknown")
	require.False(t, handled)
	require.Equal(t, http.StatusOK, w.Code)
	require.Empty(t, w.Header().Get("Location"))
}
