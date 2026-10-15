package oauthserver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/habitat-network/habitat/internal/clientmetadata"
	"github.com/habitat-network/habitat/internal/db/testutil"
	keyval_testutil "github.com/habitat-network/habitat/internal/keyval/testutil"
	"github.com/ory/fosite"
	"github.com/stretchr/testify/require"
)

func TestGetClient(t *testing.T) {
	store, err := newStore(
		testutil.NewDB(t, Models()),
		nil,
		clientmetadata.NewResolver(),
		keyval_testutil.New(t),
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
		keyval_testutil.New(t),
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
		testutil.NewDB(t, Models()), nil, clientmetadata.NewResolver(), keyval_testutil.New(t),
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

// A refresh token session survives the keyval round trip with the expiry the
// token endpoint reads back, records the connected app, and is gone once
// rotated.
func TestStoreRefreshTokenSessionRoundTrip(t *testing.T) {
	store, err := newStore(
		testutil.NewDB(t, Models()), nil, clientmetadata.NewResolver(), keyval_testutil.New(t),
	)
	require.NoError(t, err)
	ctx := t.Context()

	clientID := "http://localhost/?redirect_uri=" + url.QueryEscape("http://127.0.0.1/callback")
	client, err := store.GetClient(ctx, clientID)
	require.NoError(t, err)
	expiresAt := time.Now().Add(time.Hour).Truncate(time.Second)
	req := &fosite.Request{
		Client: client,
		Session: &session{
			Subject:               "did:plc:one",
			Scopes:                []string{"a", "b"},
			RefreshTokenExpiresAt: expiresAt,
		},
	}

	require.NoError(t, store.CreateRefreshTokenSession(ctx, "rt-sig", "at-sig", req))

	got, err := store.GetRefreshTokenSession(ctx, "rt-sig", nil)
	require.NoError(t, err)
	gotSession, ok := got.GetSession().(*session)
	require.True(t, ok)
	require.Equal(t, "did:plc:one", gotSession.Subject)
	require.Equal(t, []string{"a", "b"}, gotSession.Scopes)
	require.WithinDuration(t, expiresAt, gotSession.RefreshTokenExpiresAt, time.Second)

	var app ConnectedApp
	require.NoError(t, store.db.First(&app, "subject = ?", "did:plc:one").Error)
	require.Equal(t, "a b", app.Scopes)

	require.NoError(t, store.RotateRefreshToken(ctx, "request-id", "rt-sig"))
	_, err = store.GetRefreshTokenSession(ctx, "rt-sig", nil)
	require.ErrorIs(t, err, fosite.ErrNotFound)
}

// A refresh token session past its expiry reads as missing.
func TestStoreRefreshTokenSessionExpired(t *testing.T) {
	store, err := newStore(
		testutil.NewDB(t, Models()), nil, clientmetadata.NewResolver(), keyval_testutil.New(t),
	)
	require.NoError(t, err)
	ctx := t.Context()

	clientID := "http://localhost/?redirect_uri=" + url.QueryEscape("http://127.0.0.1/callback")
	client, err := store.GetClient(ctx, clientID)
	require.NoError(t, err)
	req := &fosite.Request{
		Client: client,
		Session: &session{
			Subject:               "did:plc:one",
			RefreshTokenExpiresAt: time.Now().Add(-time.Minute),
		},
	}
	require.NoError(t, store.CreateRefreshTokenSession(ctx, "rt-sig", "at-sig", req))

	_, err = store.GetRefreshTokenSession(ctx, "rt-sig", nil)
	require.ErrorIs(t, err, fosite.ErrNotFound)
}
