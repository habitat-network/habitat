package oauthclient

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/singleflight"
)

func TestRoundTripper(t *testing.T) {
	oauthServer := testOauthServer(t)
	resourceServer := testResourceServer(t)

	store := oauth.NewMemStore()
	err := store.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:   "did:web:example.com",
		SessionID:    "session",
		AccessToken:  newJwtToken(t, "old", time.Now().Add(-time.Hour)), // expired
		RefreshToken: "refresh-token",
		HostURL:      oauthServer.URL,
	})
	require.NoError(t, err)

	config := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)

	var refreshG singleflight.Group

	client, err := NewClient(
		t.Context(),
		store,
		&config,
		"did:web:example.com",
		"session",
		&refreshG,
	)
	require.NoError(t, err)

	// test concurrent refresh attempts
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			resp, err := client.Get(resourceServer.URL + "/xrpc/test.xrpc.endpoint")
			defer func() { require.NoError(t, resp.Body.Close()) }()
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)
		})
	}
	wg.Wait()

	sess, err := store.GetSession(t.Context(), "did:web:example.com", "session")
	require.NoError(t, err)
	require.Equal(t, "new-refresh-token", sess.RefreshToken)
}

func testOauthServer(t *testing.T) *httptest.Server {
	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// refresh token should only be called once
		require.True(t, called.CompareAndSwap(false, true))
		require.Equal(t, "/oauth/token", r.URL.Path)
		require.Equal(t, "POST", r.Method)
		require.NoError(t, r.ParseForm())
		require.Equal(t, "refresh-token", r.PostForm.Get("refresh_token"))
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]string{
			"access_token":  newJwtToken(t, "new", time.Now().Add(time.Hour)),
			"refresh_token": "new-refresh-token",
		}))
	}))
	t.Cleanup(server.Close)
	return server
}

func testResourceServer(t *testing.T) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "/xrpc/test.xrpc.endpoint", r.URL.Path)
		var claims jwt.MapClaims
		_, _, err := jwt.NewParser().
			ParseUnverified(r.Header.Get("Authorization")[len("Bearer "):], &claims)
		require.NoError(t, err)
		require.Equal(t, "new", claims["jti"])
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server
}

func newJwtToken(t *testing.T, jti string, exp time.Time) string {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	token, err := jwt.NewWithClaims(
		jwt.SigningMethodPS256,
		jwt.MapClaims{"exp": exp.Unix(), "jti": jti},
	).SignedString(key)
	require.NoError(t, err)
	return token
}
