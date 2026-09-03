package clientmeta

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
}
