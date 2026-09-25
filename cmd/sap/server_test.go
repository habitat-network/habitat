package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/pkg/oauthclient"
	"github.com/habitat-network/habitat/pkg/sap"
	"github.com/stretchr/testify/require"
)

// newTestServer wires up a sap server with a fresh in-memory-backed OAuth
// client app, suitable for exercising handleAddSession/handleOAuthCallback
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

	s, err := sap.New(sap.Config{DB: db, OAuthClient: oauthApp, Directory: oauthApp.Dir})
	require.NoError(t, err)

	return NewSapServer(s, oauthApp, "https://example.com", ConfiguredClientMetadata{})
}

func TestHandleAddSessionWithoutReturnToUnaffected(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	body, err := json.Marshal(map[string]string{"handle": "not a valid handle!!"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/session/add", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleAddSession(w, req)

	// An unresolvable/invalid handle fails fast inside StartAuthFlow (no
	// network call), same as before this change.
	require.Equal(t, http.StatusInternalServerError, w.Code)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.Empty(t, srv.pendingLogins)
}

func TestHandleAddSessionStoresReturnToForResolvedDID(t *testing.T) {
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
		"state":     "nonce123",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/session/add", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleAddSession(w, req)

	// StartAuthFlow itself still fails (no PDS host), but our own
	// resolution should have already stored the pending return_to.
	require.Equal(t, http.StatusInternalServerError, w.Code)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.Equal(t, pendingLogin{
		returnTo: "https://app.example.com/callback",
		state:    "nonce123",
	}, srv.pendingLogins[testDID.String()])
}

func redeem(t *testing.T, srv *server, code string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"code": code})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/session/redeem", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleRedeemLogin(w, req)
	return w
}

func TestRedirectToReturnToRedirectsWithRedeemableCode(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	const testDID = "did:plc:testreturnto"

	srv.mu.Lock()
	srv.pendingLogins[testDID] = pendingLogin{
		returnTo: "https://app.example.com/callback",
		state:    "nonce123",
	}
	srv.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/oauth-callback", http.NoBody)
	w := httptest.NewRecorder()

	handled := srv.redirectToReturnTo(w, req, testDID)
	require.True(t, handled)
	require.Equal(t, http.StatusSeeOther, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "app.example.com", loc.Host)
	require.Equal(t, "/callback", loc.Path)
	require.Empty(t, loc.Query().Get("did"))
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)

	srv.mu.Lock()
	require.Empty(t, srv.pendingLogins)
	srv.mu.Unlock()

	rw := redeem(t, srv, code)
	require.Equal(t, http.StatusOK, rw.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(rw.Body.Bytes(), &resp))
	require.Equal(t, map[string]string{"did": testDID, "state": "nonce123"}, resp)
}

func TestHandleRedeemLoginRejectsForgedOrExpiredCode(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	expired, err := srv.sealLoginCode(loginCode{
		DID:     "did:plc:alice",
		Expires: time.Now().Add(-time.Second).Unix(),
	})
	require.NoError(t, err)

	// Same payload shape, sealed by a different sap (a different key).
	other := newTestServer(t)
	forged, err := other.sealLoginCode(loginCode{
		DID:     "did:plc:alice",
		Expires: time.Now().Add(time.Minute).Unix(),
	})
	require.NoError(t, err)

	// A valid code with one byte flipped.
	valid, err := srv.sealLoginCode(loginCode{
		DID:     "did:plc:alice",
		Expires: time.Now().Add(time.Minute).Unix(),
	})
	require.NoError(t, err)
	raw, err := base64.RawURLEncoding.DecodeString(valid)
	require.NoError(t, err)
	raw[len(raw)-1] ^= 0x01
	tampered := base64.RawURLEncoding.EncodeToString(raw)

	for name, code := range map[string]string{
		"garbage":  "not-a-code",
		"expired":  expired,
		"forged":   forged,
		"tampered": tampered,
	} {
		require.Equal(t, http.StatusNotFound, redeem(t, srv, code).Code, name)
	}

	req := httptest.NewRequest(http.MethodGet, "/session/redeem", http.NoBody)
	w := httptest.NewRecorder()
	srv.handleRedeemLogin(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestHandleListSessions(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	const testDID = syntax.DID("did:plc:testlistorgs")
	const testSessionID = "session-abc"
	require.NoError(t, srv.sap.AddSession(t.Context(), testDID, testSessionID))

	req := httptest.NewRequest(http.MethodGet, "/session/list", http.NoBody)
	w := httptest.NewRecorder()

	srv.handleListSessions(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Sessions []string `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, []string{string(testDID)}, body.Sessions)
}

func TestRedirectToReturnToNoPendingFallsBackToFalse(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/oauth-callback", http.NoBody)
	w := httptest.NewRecorder()

	handled := srv.redirectToReturnTo(w, req, "did:plc:unknown")
	require.False(t, handled)
	require.Equal(t, http.StatusOK, w.Code)
	require.Empty(t, w.Header().Get("Location"))
}

func TestHandleClientMetadataIncludesJWKSForConfidentialClient(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	priv, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	require.NoError(t, srv.oauthClient.Config.SetClientSecret(priv, "sap"))

	req := httptest.NewRequest(http.MethodGet, "/client-metadata.json", http.NoBody)
	w := httptest.NewRecorder()

	srv.handleClientMetadata(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var metadata oauth.ClientMetadata
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
	require.Equal(t, "private_key_jwt", metadata.TokenEndpointAuthMethod)
	require.NotNil(t, metadata.JWKS)
	require.Len(t, metadata.JWKS.Keys, 1)
}

func TestHandleTrackSpaceRequiresDIDHeader(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	body, err := json.Marshal(map[string]string{
		"space": "at://did:plc:owner/space/network.habitat.docs/abc",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/space/track", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleTrackSpace(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleTrackSpaceAllowsMissingSessionHeader pins that the session
// header is optional — it resolves fine under WithSingleSessionPerUser, and
// a caller that doesn't track one can just omit it. The request still fails
// here (no session is tracked for the DID at all in this test), but not
// because the header itself is missing — confirmed by the error not being a
// 400 about the header.
func TestHandleTrackSpaceAllowsMissingSessionHeader(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	body, err := json.Marshal(map[string]string{
		"space": "at://did:plc:owner/space/network.habitat.docs/abc",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/space/track", bytes.NewReader(body))
	req.Header.Set(habitatDIDHeader, "did:plc:member")
	w := httptest.NewRecorder()
	srv.handleTrackSpace(w, req)
	require.NotEqual(t, http.StatusBadRequest, w.Code)
}

func TestHandleRecrawlRequiresDIDHeader(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/session/recrawl", http.NoBody)
	w := httptest.NewRecorder()
	srv.handleRecrawl(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandleRecrawlAllowsMissingSessionHeader pins that the session header is
// optional, mirroring handleTrackSpace: it resolves fine under
// WithSingleSessionPerUser, and a caller that doesn't track one can just omit
// it.
func TestHandleRecrawlAllowsMissingSessionHeader(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/session/recrawl", http.NoBody)
	req.Header.Set(habitatDIDHeader, "did:plc:member")
	w := httptest.NewRecorder()
	srv.handleRecrawl(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)
}

// TestHandleRecrawlSchedulesAndReturnsAccepted pins that a well-formed
// request returns 202 immediately — Recrawl schedules the crawl in the
// background rather than the handler waiting for it to finish.
func TestHandleRecrawlSchedulesAndReturnsAccepted(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/session/recrawl", http.NoBody)
	req.Header.Set(habitatDIDHeader, "did:plc:member")
	req.Header.Set(habitatSessionHeader, "session-abc")
	w := httptest.NewRecorder()
	srv.handleRecrawl(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)
}

// TestHandleSpaceCredential covers /space/credential's error paths: a missing
// or unparsable space is a bad request, and a space no tracked session can
// access fails to mint. The success path is TestSapSpaceCredential.
func TestHandleSpaceCredential(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		query    string
		wantCode int
	}{
		{"missing space", "", http.StatusBadRequest},
		{"unparsable space", "?space=not-a-space", http.StatusBadRequest},
		{
			"no session can access the space",
			"?space=at://did:plc:owner/space/network.habitat.docs/abc",
			http.StatusBadGateway,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := newTestServer(t)
			req := httptest.NewRequest(http.MethodGet, "/space/credential"+tt.query, http.NoBody)
			w := httptest.NewRecorder()
			srv.handleSpaceCredential(w, req)
			require.Equal(t, tt.wantCode, w.Code, w.Body.String())
		})
	}
}

func TestHandleNotifyWriteRejectsMissingOrInvalidAuth(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	body, err := json.Marshal(map[string]string{
		"space": "at://did:plc:owner/space/network.habitat.docs/abc",
		"repo":  "did:plc:member",
		"rev":   "3jzfcijpj2z2a",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.notifyWrite",
		bytes.NewReader(body),
	)
	w := httptest.NewRecorder()
	srv.handleNotifyWrite(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	req = httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.notifyWrite",
		bytes.NewReader(body),
	)
	req.Header.Set("Authorization", "Bearer not-a-real-jwt")
	w = httptest.NewRecorder()
	srv.handleNotifyWrite(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestBasicAuthMiddlewareRejectsMissingOrWrongPassword(t *testing.T) {
	t.Parallel()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := basicAuthMiddleware("s3cret", next)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	req = httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	req.SetBasicAuth("anyone", "wrong")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestBasicAuthMiddlewareAllowsAnyUsernameWithCorrectPassword(t *testing.T) {
	t.Parallel()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := basicAuthMiddleware("s3cret", next)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	req.SetBasicAuth("whoever", "s3cret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestHandleTrackSpaceRejectsUnparsableSpace(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	body, err := json.Marshal(map[string]string{"space": "not a uri"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/space/track", bytes.NewReader(body))
	req.Header.Set(habitatDIDHeader, "did:plc:member")
	req.Header.Set(habitatSessionHeader, "session-abc")
	w := httptest.NewRecorder()

	srv.handleTrackSpace(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
