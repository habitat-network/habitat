package clientmetadata

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolverFetchMetadataLocalhost(t *testing.T) {
	r := NewResolver()

	t.Run("defaults", func(t *testing.T) {
		metadata, err := r.FetchMetadata(context.Background(), "http://localhost")
		require.NoError(t, err)

		require.Equal(t, "http://localhost", metadata.ClientID)
		require.Equal(t, "none", metadata.TokenEndpointAuthMethod)
		require.Equal(t, []string{"http://127.0.0.1/", "http://[::1]/"}, metadata.RedirectURIs)
		require.Equal(t, "atproto", metadata.Scope)
		require.Equal(t, []string{"code"}, metadata.ResponseTypes)
		require.Equal(t, []string{"authorization_code", "refresh_token"}, metadata.GrantTypes)
	})

	t.Run("query parameters", func(t *testing.T) {
		clientId := "http://localhost/?" + url.Values{
			"redirect_uri": {"http://127.0.0.1/callback", "http://[::1]/callback"},
			"scope":        {"atproto transition:generic"},
		}.Encode()

		metadata, err := r.FetchMetadata(context.Background(), clientId)
		require.NoError(t, err)

		require.Equal(t, clientId, metadata.ClientID)
		require.Equal(
			t,
			[]string{"http://127.0.0.1/callback", "http://[::1]/callback"},
			metadata.RedirectURIs,
		)
		require.Equal(t, "atproto transition:generic", metadata.Scope)
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
				_, err := r.FetchMetadata(context.Background(), clientId)
				require.Error(t, err)
			})
		}
	})
}
