package pearserver_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/stretchr/testify/require"

	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
)

func TestServer_GetClientMetadata(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("fetches the metadata document server-side", func(t *testing.T) {
		// The upstream metadata server intentionally sends no CORS headers:
		// the whole point of the proxy is that the browser never talks to it
		// directly.
		doc := oauth.ClientMetadata{
			ClientName:              new("Example App"),
			ClientURI:               new("https://example.com/"),
			LogoURI:                 new("https://example.com/logo.png"),
			PolicyURI:               new("https://example.com/privacy"),
			TosURI:                  new("https://example.com/terms"),
			Scope:                   "atproto org:network.habitat.note",
			RedirectURIs:            []string{"https://example.com/callback"},
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			TokenEndpointAuthMethod: "none",
			DPoPBoundAccessTokens:   true,
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(doc))
		}))
		defer server.Close()

		var got oauth.ClientMetadata
		code := client.Query(
			ts.Server.GetClientMetadata,
			url.Values{"client_id": {server.URL + "/client-metadata.json"}},
			&got,
		)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, doc, got)
	})

	t.Run("missing client_id is a bad request", func(t *testing.T) {
		var body atclient.ErrorBody
		code := client.Query(ts.Server.GetClientMetadata, url.Values{}, &body)
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("unreachable metadata server is a bad gateway", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "no", http.StatusInternalServerError)
		}))
		defer server.Close()

		var body atclient.ErrorBody
		code := client.Query(
			ts.Server.GetClientMetadata,
			url.Values{"client_id": {server.URL + "/client-metadata.json"}},
			&body,
		)
		require.Equal(t, http.StatusBadGateway, code)
	})

	t.Run("localhost client id metadata is synthesized", func(t *testing.T) {
		var got oauth.ClientMetadata
		code := client.Query(
			ts.Server.GetClientMetadata,
			url.Values{"client_id": {"http://localhost/?redirect_uri=http://127.0.0.1/callback&scope=atproto"}},
			&got,
		)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "Development client", *got.ClientName)
	})
}
