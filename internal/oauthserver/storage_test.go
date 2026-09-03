package oauthserver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/habitat-network/habitat/internal/clientmetadata"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/ory/fosite"
	"github.com/stretchr/testify/require"
)

func TestGetClient(t *testing.T) {
	store, err := newStore(testutil.NewDB(t), nil, clientmetadata.NewResolver())
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

// TestGetClientRedirectUriPortNotMatched exercises fosite's own RFC 8252
// §7.3 loopback redirect URI matching (fosite.MatchRedirectURIWithClientRedirectURIs)
// against a localhost dev client whose redirect_uri carries no port. The
// localhost-metadata-synthesis behavior itself is covered by
// clientmetadata.TestResolverFetchMetadataLocalhost.
func TestGetClientRedirectUriPortNotMatched(t *testing.T) {
	store, err := newStore(testutil.NewDB(t), nil, clientmetadata.NewResolver())
	require.NoError(t, err)

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
}
