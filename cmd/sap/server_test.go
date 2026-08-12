package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/pkg/oauthclient"
	"github.com/habitat-network/habitat/pkg/sap"
	"github.com/stretchr/testify/require"
)

// fakeDirectory always fails to resolve: the tests below only exercise
// Store.Add via AddSession, but AddSession also kicks off a background crawl
// that will call this directory to build a client. Erroring cleanly (rather
// than e.g. returning a nil identity) keeps that background failure from
// panicking the test binary.
type fakeDirectory struct{}

func (fakeDirectory) LookupDID(context.Context, syntax.DID) (*identity.Identity, error) {
	return nil, errors.New("fakeDirectory: not implemented")
}

func (fakeDirectory) LookupHandle(context.Context, syntax.Handle) (*identity.Identity, error) {
	return nil, errors.New("fakeDirectory: not implemented")
}

func (fakeDirectory) Lookup(context.Context, syntax.AtIdentifier) (*identity.Identity, error) {
	return nil, errors.New("fakeDirectory: not implemented")
}

func (fakeDirectory) Purge(context.Context, syntax.AtIdentifier) error { return nil }

// newTestServer wires a minimal sap + oauth/jwt-bearer client + server, same
// shape as cmd/sap/main.go, for handler-level tests that don't need a live
// host. The client's directory is faked so a background crawl triggered by
// AddSession fails cleanly instead of reaching the real network.
func newTestServer(t *testing.T) *server {
	t.Helper()

	db := testutil.NewDB(t)
	store, err := oauthclient.NewGormStore(db)
	require.NoError(t, err)

	cfg := oauth.NewPublicConfig(
		"https://sap.example/client-metadata.json",
		"https://sap.example/oauth-callback",
		[]string{},
	)
	key, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	require.NoError(t, cfg.SetClientSecret(key, "sap"))
	oauthApp := oauth.NewClientApp(&cfg, store)
	oauthApp.Dir = fakeDirectory{}
	jwtClient := oauthclient.NewJWTBearerClient(oauthApp)

	sessions, err := newSessionResolver(db, jwtClient)
	require.NoError(t, err)

	s, err := sap.New(sap.Config{DB: db, Clients: sessions})
	require.NoError(t, err)

	return NewSapServer(s, sessions, jwtClient, "sap.example", nil)
}

func TestHandleClientMetadataServesConfidentialClientDocument(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/client-metadata.json", nil)
	w := httptest.NewRecorder()
	srv.handleClientMetadata(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var got oauth.ClientMetadata
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))

	require.Equal(t, "https://sap.example/client-metadata.json", got.ClientID)
	require.Equal(t, "private_key_jwt", got.TokenEndpointAuthMethod)
	require.Equal(t, "ES256", *got.TokenEndpointAuthSigningAlg)
	require.True(t, got.DPoPBoundAccessTokens)
	require.Equal(t, "atproto", got.Scope)
	require.Contains(t, got.GrantTypes, "urn:ietf:params:oauth:grant-type:jwt-bearer")
	require.NotNil(t, got.JWKS)
	require.Len(t, got.JWKS.Keys, 1)
}

func TestHandleAddJWTSessionAddsSession(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	did := "did:web:member.example"
	body := strings.NewReader(`{"did":"` + did + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/session/jwt", body)
	w := httptest.NewRecorder()
	srv.handleAddJWTSession(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	sessions, err := srv.sap.Sessions(t.Context())
	require.NoError(t, err)
	require.Contains(t, sessions, syntax.DID(did))
}

func TestHandleAddJWTSessionRejectsInvalidDID(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	req := httptest.NewRequest(
		http.MethodPost,
		"/session/jwt",
		strings.NewReader(`{"did":"not-a-did"}`),
	)
	w := httptest.NewRecorder()
	srv.handleAddJWTSession(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
