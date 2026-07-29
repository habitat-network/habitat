package oauthclient

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/stretchr/testify/require"
)

// testDPoPKey returns a valid DPoP private key multibase so ResumeSession can
// parse the fake session data.
func testDPoPKey(t *testing.T) string {
	t.Helper()
	key, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	return key.Multibase()
}

func testAccessToken(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tok, err := jwt.NewWithClaims(jwt.SigningMethodPS256,
		jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix(), "jti": "test"},
	).SignedString(key)
	require.NoError(t, err)
	return tok
}

// newTestSessionGetter seeds a session for (did, sessionID) resolvable
// against a fake PDS at pdsURL, and returns a SessionGetter that can resume it.
func newTestSessionGetter(
	t *testing.T,
	did syntax.DID,
	sessionID string,
	pdsURL string,
) *SessionGetter {
	t.Helper()
	store, err := NewGormStore(testutil.NewDB(t))
	require.NoError(t, err)
	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	require.NoError(t, store.SaveSession(context.Background(), oauth.ClientSessionData{
		AccountDID:              did,
		SessionID:               sessionID,
		HostURL:                 pdsURL,
		AccessToken:             testAccessToken(t),
		DPoPPrivateKeyMultibase: testDPoPKey(t),
	}))
	return NewSessionGetter(oauth.NewClientApp(&cfg, store))
}

func TestSessionGetter_ResumeSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	did := syntax.DID("did:plc:test")
	sg := newTestSessionGetter(t, did, "sess1", srv.URL)

	sess, err := sg.ResumeSession(context.Background(), did, "sess1")
	require.NoError(t, err)
	defer sess.Close()
	require.Equal(t, did, sess.Data.AccountDID)

	client := sess.AuthClient(syntax.NSID("network.habitat.test"))
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/xrpc/network.habitat.test", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSessionGetter_Do(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	did := syntax.DID("did:plc:test")
	sg := newTestSessionGetter(t, did, "sess1", srv.URL)

	req, err := http.NewRequest(http.MethodGet, "/xrpc/com.atproto.repo.getRecord", nil)
	require.NoError(t, err)
	resp, err := sg.Do(context.Background(), did, "sess1", req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "/xrpc/com.atproto.repo.getRecord", gotPath)
	require.Contains(t, gotAuth, "DPoP")
}

func TestSessionGetter_DoNoSession(t *testing.T) {
	store, err := NewGormStore(testutil.NewDB(t))
	require.NoError(t, err)
	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	sg := NewSessionGetter(oauth.NewClientApp(&cfg, store))

	req, err := http.NewRequest(http.MethodGet, "/xrpc/com.atproto.repo.getRecord", nil)
	require.NoError(t, err)
	resp, err := sg.Do(context.Background(), "did:plc:missing", "sess1", req)
	require.Error(t, err)
	if resp != nil {
		_ = resp.Body.Close()
	}
}

func TestSessionGetter_ResumeSessionNotFound(t *testing.T) {
	store, err := NewGormStore(testutil.NewDB(t))
	require.NoError(t, err)
	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	sg := NewSessionGetter(oauth.NewClientApp(&cfg, store))

	_, err = sg.ResumeSession(context.Background(), "did:plc:missing", "sess1")
	require.Error(t, err)
}

func TestSessionGetter_CachesWhileHeld(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	did := syntax.DID("did:plc:test")
	sg := newTestSessionGetter(t, did, "sess1", srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	first, err := sg.ResumeSession(ctx, did, "sess1")
	require.NoError(t, err)
	defer first.Close()

	second, err := sg.ResumeSession(ctx, did, "sess1")
	require.NoError(t, err)
	defer second.Close()

	// Each call gets its own handle, but both wrap the same underlying
	// resumed session while the cache entry is still alive.
	require.NotSame(t, first, second)
	require.Same(t, first.ClientSession, second.ClientSession)
}

func TestSessionGetter_EvictsOnceUnheld(t *testing.T) {
	did := syntax.DID("did:plc:test")
	// No real network round trip is needed for this test (only ResumeSession's
	// store lookup, not an authenticated request), so this runs inside a
	// synctest bubble for deterministic goroutine scheduling instead of
	// polling wall-clock time.
	sg := newTestSessionGetter(t, did, "sess1", "https://pds.example.com")

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		first, err := sg.ResumeSession(ctx, did, "sess1")
		require.NoError(t, err)
		first.Close()
		cancel()

		// Let the eviction goroutines (wg.Wait -> Delete, and ctx.Done -> wg.Done)
		// run to completion before observing cache state.
		synctest.Wait()

		_, ok := sg.sessions.Load(cacheKey(did, "sess1"))
		require.False(t, ok, "session was not evicted from the cache")

		second, err := sg.ResumeSession(context.Background(), did, "sess1")
		require.NoError(t, err)
		defer second.Close()
		require.NotSame(t, first.ClientSession, second.ClientSession)
	})
}
