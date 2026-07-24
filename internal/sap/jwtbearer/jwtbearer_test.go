package jwtbearer

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

type fakeDir struct{ hostURL string }

func (f fakeDir) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	return &identity.Identity{
		DID: did,
		Services: map[string]identity.ServiceEndpoint{
			"habitat": {Type: "HabitatServer", URL: f.hostURL},
		},
	}, nil
}

func TestClientForDIDMintsAndAttachesToken(t *testing.T) {
	var tokenRequests int64
	var seenAuth, seenMethod string

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&tokenRequests, 1)
		require.NoError(t, r.ParseForm())
		require.Equal(t, "urn:ietf:params:oauth:grant-type:jwt-bearer", r.Form.Get("grant_type"))
		require.NotEmpty(t, r.Form.Get("assertion"))
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok-123", "expires_in": 3600})
	})
	mux.HandleFunc(
		"/xrpc/network.habitat.space.listSpaces",
		func(w http.ResponseWriter, r *http.Request) {
			seenAuth = r.Header.Get("Authorization")
			seenMethod = r.Header.Get("Habitat-Auth-Method")
			_ = json.NewEncoder(w).Encode(map[string]any{"spaces": []any{}})
		},
	)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	b := New("https://sap.example/client-metadata.json", key, fakeDir{hostURL: server.URL})

	did := syntax.DID("did:web:member.example")
	client, err := b.ClientForDID(t.Context(), did)
	require.NoError(t, err)

	for range 3 {
		resp, err := client.Get("/xrpc/network.habitat.space.listSpaces")
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		_ = resp.Body.Close()
	}

	require.Equal(t, "Bearer tok-123", seenAuth)
	require.Equal(t, "oauth", seenMethod)
	require.Equal(
		t,
		int64(1),
		atomic.LoadInt64(&tokenRequests),
		"token should be cached across requests",
	)
	require.True(t, strings.HasPrefix(b.PublicJWK().Algorithm, "ES256"))
}

// TestClientForDIDConcurrentGetsMintOnce proves concurrent requests for the
// same DID are deduped by singleflight rather than each triggering their own
// mint: N goroutines race to fetch a token for one DID while the mint
// endpoint is held open until all of them have started, and only one
// /oauth/token request should ever be observed.
func TestClientForDIDConcurrentGetsMintOnce(t *testing.T) {
	const n = 20

	var tokenRequests int64
	var started sync.WaitGroup
	started.Add(n)
	release := make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&tokenRequests, 1)
		// Hold the response open until every goroutine has started its
		// request, so they all contend on the singleflight call for this
		// DID instead of racing serially.
		<-release
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok-concurrent", "expires_in": 3600})
	})
	mux.HandleFunc(
		"/xrpc/network.habitat.space.listSpaces",
		func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"spaces": []any{}})
		},
	)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	b := New("https://sap.example/client-metadata.json", key, fakeDir{hostURL: server.URL})

	did := syntax.DID("did:web:member.example")
	client, err := b.ClientForDID(t.Context(), did)
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			started.Done()
			resp, err := client.Get("/xrpc/network.habitat.space.listSpaces")
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			_ = resp.Body.Close()
		}()
	}

	started.Wait()
	close(release)
	wg.Wait()

	require.Equal(
		t,
		int64(1),
		atomic.LoadInt64(&tokenRequests),
		"concurrent requests for the same DID should mint exactly once",
	)
}
