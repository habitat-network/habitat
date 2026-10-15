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
	"github.com/habitat-network/habitat/internal/keyval"
	"github.com/ory/fosite"
	"github.com/stretchr/testify/require"
)

// testKV returns a keyval store, namespaced as pear namespaces it for the OAuth
// server, over its own throwaway database.
func testKV(t *testing.T) keyval.Store {
	t.Helper()
	kv, err := keyval.New(t.Context(), testutil.NewDB(t, keyval.Models()))
	require.NoError(t, err)
	return kv.Namespace("oauth")
}

func TestGetClient(t *testing.T) {
	store, err := newStore(
		testutil.NewDB(t, Models()),
		nil,
		clientmetadata.NewResolver(),
		testKV(t),
	)
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
	store, err := newStore(
		testutil.NewDB(t, Models()),
		nil,
		clientmetadata.NewResolver(),
		testKV(t),
	)
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

// A pushed authorization request survives the keyval round trip with its
// subject update, and is gone once deleted.
func TestStorePARSessionRoundTrip(t *testing.T) {
	store, err := newStore(
		testutil.NewDB(t, Models()), nil, clientmetadata.NewResolver(), testKV(t),
	)
	require.NoError(t, err)
	ctx := t.Context()

	clientID := "http://localhost/?redirect_uri=" + url.QueryEscape("http://127.0.0.1/callback")
	client, err := store.GetClient(ctx, clientID)
	require.NoError(t, err)
	req := &fosite.AuthorizeRequest{
		ResponseTypes: fosite.Arguments{"code"},
		Request: fosite.Request{
			Client:         client,
			RequestedScope: fosite.Arguments{"a", "b"},
			Session:        &session{Subject: "did:plc:one"},
			Form: url.Values{
				"code_challenge":        {"challenge"},
				"code_challenge_method": {"S256"},
				"redirect_uri":          {"http://127.0.0.1:5173/callback"},
				"state":                 {"st"},
			},
		},
	}

	require.NoError(t, store.CreatePARSession(ctx, "urn:req:1", req))
	require.NoError(t, store.UpdatePARSessionSubject(ctx, "urn:req:1", "did:plc:two"))

	got, err := store.GetPARSession(ctx, "urn:req:1")
	require.NoError(t, err)
	require.Equal(t, "did:plc:two", got.GetSession().GetSubject())
	require.Equal(t, "st", got.GetState())
	require.Equal(t, "challenge", got.GetRequestForm().Get("code_challenge"))

	pkceReq, err := store.GetPKCERequestSession(ctx, "urn:req:1", nil)
	require.NoError(t, err)
	require.Equal(t, "S256", pkceReq.GetRequestForm().Get("code_challenge_method"))

	require.NoError(t, store.DeletePARSession(ctx, "urn:req:1"))
	_, err = store.GetPARSession(ctx, "urn:req:1")
	require.ErrorIs(t, err, fosite.ErrNotFound)
	// Updating or deleting a session that is gone is not an error.
	require.NoError(t, store.UpdatePARSessionSubject(ctx, "urn:req:1", "did:plc:two"))
	require.NoError(t, store.DeletePARSession(ctx, "urn:req:1"))
}
