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

func TestResolverResolveAtprotoKeyInlineJWKS(t *testing.T) {
	jwk, pub := testJWK(t, "key-1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(oauth.ClientMetadata{
			ClientID: "http://" + r.Host + "/client-metadata.json",
			JWKS:     &oauth.JWKS{Keys: []atcrypto.JWK{jwk}},
		}))
	}))
	defer server.Close()

	key, err := NewResolver().ResolveAtprotoKey(
		context.Background(), server.URL+"/client-metadata.json", "key-1",
	)
	require.NoError(t, err)
	require.True(t, key.Equal(pub))
}

func TestResolverResolveAtprotoKeyJWKSURI(t *testing.T) {
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
}

func TestResolverResolveAtprotoKeyNotFound(t *testing.T) {
	jwk, _ := testJWK(t, "key-1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(oauth.ClientMetadata{
			ClientID: "http://" + r.Host + "/client-metadata.json",
			JWKS:     &oauth.JWKS{Keys: []atcrypto.JWK{jwk}},
		}))
	}))
	defer server.Close()

	_, err := NewResolver().ResolveAtprotoKey(
		context.Background(), server.URL+"/client-metadata.json", "wrong-kid",
	)
	require.ErrorIs(t, err, ErrKeyNotFound)
}

// TestResolverResolveAtprotoKeyTimesOut covers the hot-path DoS/latency-coupling
// finding: a slow or hanging metadata host must not stall ResolveAtprotoKey
// indefinitely (bounded only by the inbound request's own context) — the
// Resolver's own timeout must cut the fetch off. Uses a short timeout via
// NewResolverWithTimeout rather than waiting out the real production default
// (defaultFetchTimeout) in the test suite.
func TestResolverResolveAtprotoKeyTimesOut(t *testing.T) {
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

	const testTimeout = 20 * time.Millisecond
	start := time.Now()
	_, err := NewResolverWithTimeout(testTimeout).ResolveAtprotoKey(
		context.Background(), server.URL+"/client-metadata.json", "key-1",
	)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Less(
		t, elapsed, 2*time.Second,
		"ResolveAtprotoKey should be bounded by the resolver's own timeout, not hang",
	)
}

func TestResolverResolveAtprotoKeyNoJWKS(t *testing.T) {
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
}
