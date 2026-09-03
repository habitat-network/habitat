package clientmeta

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/httpx"
)

func testJWK(t *testing.T, kid string) (atcrypto.JWK, atcrypto.PublicKey) {
	t.Helper()
	priv, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	pub, err := priv.PublicKey()
	require.NoError(t, err)
	jwk, err := pub.JWK()
	require.NoError(t, err)
	jwk.KeyID = &kid
	return *jwk, pub
}

func TestResolverResolveAtprotoKey(t *testing.T) {
	t.Run("inline jwks", func(t *testing.T) {
		jwk, pub := testJWK(t, "key-1")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(oauth.ClientMetadata{
				ClientID: "http://" + r.Host + "/client-metadata.json",
				JWKS:     &oauth.JWKS{Keys: []atcrypto.JWK{jwk}},
			}))
		}))
		defer server.Close()
		clientID := server.URL + "/client-metadata.json"

		t.Run("resolves the key", func(t *testing.T) {
			key, err := NewResolver().ResolveAtprotoKey(context.Background(), clientID, "key-1")
			require.NoError(t, err)
			require.True(t, key.Equal(pub))
		})

		t.Run("unknown kid returns not found", func(t *testing.T) {
			_, err := NewResolver().ResolveAtprotoKey(context.Background(), clientID, "wrong-kid")
			require.ErrorIs(t, err, ErrKeyNotFound)
		})
	})

	t.Run("jwks via jwks_uri resolves the key", func(t *testing.T) {
		jwk, pub := testJWK(t, "key-1")

		var jwksURL string
		mux := http.NewServeMux()
		mux.HandleFunc("/client-metadata.json", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(oauth.ClientMetadata{
				ClientID: "http://" + r.Host + "/client-metadata.json",
				JWKSURI:  &jwksURL,
			}))
		})
		mux.HandleFunc("/jwks.json", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(oauth.JWKS{Keys: []atcrypto.JWK{jwk}}))
		})
		server := httptest.NewServer(mux)
		defer server.Close()
		jwksURL = server.URL + "/jwks.json"

		key, err := NewResolver().ResolveAtprotoKey(
			context.Background(), server.URL+"/client-metadata.json", "key-1",
		)
		require.NoError(t, err)
		require.True(t, key.Equal(pub))
	})

	t.Run("no jwks published returns not found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(oauth.ClientMetadata{
				ClientID: "http://" + r.Host + "/client-metadata.json",
			}))
		}))
		defer server.Close()

		_, err := NewResolver().ResolveAtprotoKey(
			context.Background(), server.URL+"/client-metadata.json", "key-1",
		)
		require.ErrorIs(t, err, ErrKeyNotFound)
	})

	// Covers the hot-path DoS/latency-coupling finding: a slow or hanging
	// metadata host must not stall ResolveAtprotoKey indefinitely (bounded
	// only by the inbound request's own context) — the resolver's own client
	// timeout must cut the fetch off. Uses a short timeout via WithClient
	// rather than waiting out the real production default in the test suite.
	t.Run("times out on a hanging host", func(t *testing.T) {
		blockUntil := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-blockUntil // never responds within the test's lifetime
		}))
		// server.Close() waits for the in-flight handler to return, so
		// blockUntil must be closed (unblocking the handler) before Close is
		// called: defers run LIFO, so registering Close first and the channel
		// close second runs the channel close first.
		defer server.Close()
		defer close(blockUntil)

		cl := httpx.NewClient()
		cl.Timeout = 20 * time.Millisecond

		start := time.Now()
		_, err := NewResolver(WithClient(cl)).ResolveAtprotoKey(
			context.Background(), server.URL+"/client-metadata.json", "key-1",
		)
		elapsed := time.Since(start)

		require.Error(t, err)
		require.Less(
			t, elapsed, 2*time.Second,
			"ResolveAtprotoKey should be bounded by the resolver's own timeout, not hang",
		)
	})
}
