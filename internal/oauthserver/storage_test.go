package oauthserver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/ory/fosite"
	"github.com/stretchr/testify/require"
)

func TestGetClient(t *testing.T) {
	store, err := newStore(testutil.NewDB(t), nil)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("url: %s", r.Host)
		if r.URL.Path == "/client-metadata.json" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, err := fmt.Fprintf(w, `{
					"client_id": "%s"
				}`, "http://"+r.Host+r.URL.Path)
			require.NoError(t, err)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))

	clientId := server.URL + "/client-metadata.json"

	client, err := store.GetClient(context.Background(), clientId)
	require.NoError(t, err)

	require.Equal(t, clientId, client.GetID())
}

func TestGetClientLocalhost(t *testing.T) {
	store, err := newStore(testutil.NewDB(t), nil)
	require.NoError(t, err)

	t.Run("defaults", func(t *testing.T) {
		client, err := store.GetClient(context.Background(), "http://localhost")
		require.NoError(t, err)

		require.Equal(t, "http://localhost", client.GetID())
		require.True(t, client.IsPublic())
		require.Equal(t, []string{"http://127.0.0.1/", "http://[::1]/"}, client.GetRedirectURIs())
		require.Equal(t, fosite.Arguments{"atproto"}, client.GetScopes())
		require.Equal(t, fosite.Arguments{"code"}, client.GetResponseTypes())
		require.Equal(
			t,
			fosite.Arguments{"authorization_code", "refresh_token"},
			client.GetGrantTypes(),
		)
	})

	t.Run("query parameters", func(t *testing.T) {
		clientId := "http://localhost/?" + url.Values{
			"redirect_uri": {"http://127.0.0.1/callback", "http://[::1]/callback"},
			"scope":        {"atproto transition:generic"},
		}.Encode()

		client, err := store.GetClient(context.Background(), clientId)
		require.NoError(t, err)

		require.Equal(t, clientId, client.GetID())
		require.Equal(
			t,
			[]string{"http://127.0.0.1/callback", "http://[::1]/callback"},
			client.GetRedirectURIs(),
		)
		require.Equal(t, fosite.Arguments{"atproto", "transition:generic"}, client.GetScopes())
	})

	t.Run("redirect uri port is not matched", func(t *testing.T) {
		client, err := store.GetClient(
			context.Background(),
			"http://localhost/?redirect_uri="+url.QueryEscape("http://127.0.0.1/callback"),
		)
		require.NoError(t, err)

		matched, err := fosite.MatchRedirectURIWithClientRedirectURIs(
			"http://127.0.0.1:5173/callback",
			client,
		)
		require.NoError(t, err)
		require.Equal(t, "http://127.0.0.1:5173/callback", matched.String())
	})

	// Ids that are not localhost client ids at all (https scheme, or a
	// loopback IP rather than the localhost hostname) are not listed here:
	// those fall through to a regular client metadata document fetch.
	t.Run("rejected client ids", func(t *testing.T) {
		for _, clientId := range []string{
			"http://localhost:8080",                 // explicit port
			"http://localhost/client-metadata.json", // non-empty path
			"http://user@localhost",                 // userinfo
			"http://localhost/?scope=a&scope=b",     // multiple scope params
			"http://localhost/?foo=bar",             // unsupported parameter
			"http://localhost/?redirect_uri=" + url.QueryEscape("https://example.com/cb"),
			"http://localhost/?redirect_uri=" + url.QueryEscape("http://localhost/cb"),
		} {
			t.Run(clientId, func(t *testing.T) {
				_, err := store.GetClient(context.Background(), clientId)
				require.Error(t, err)
			})
		}
	})
}
