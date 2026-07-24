package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/sap"
	"github.com/habitat-network/habitat/internal/sap/jwtbearer"
	"github.com/stretchr/testify/require"
)

// fakeDirectory always fails to resolve: the tests below only exercise
// Store.Add via AddSession, but AddSession also kicks off a background crawl
// that will call this directory to build a client. Erroring cleanly (rather
// than e.g. returning a nil identity) keeps that background failure from
// panicking the test binary.
type fakeDirectory struct{}

func (fakeDirectory) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	return nil, errors.New("fakeDirectory: not implemented")
}

// newTestServer wires a minimal sap + jwtbearer builder + server, same shape
// as cmd/sap/main.go, for handler-level tests that don't need a live host.
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
	oauthApp := oauth.NewClientApp(&cfg, store)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	jwtBuilder := jwtbearer.New("https://sap.example/client-metadata.json", key, fakeDirectory{})

	s, err := sap.New(sap.Config{DB: db, OAuthClient: oauthApp, JWTBearer: jwtBuilder})
	require.NoError(t, err)

	return NewSapServer(s, oauthApp, jwtBuilder, "sap.example", nil)
}

func TestHandleClientMetadataServesConfidentialClientDocument(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/client-metadata.json", nil)
	w := httptest.NewRecorder()
	srv.handleClientMetadata(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var got pdsclient.ClientMetadata
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))

	require.Equal(t, "https://sap.example/client-metadata.json", got.ClientId)
	require.Equal(t, "private_key_jwt", got.TokenEndpointAuthMethod)
	require.Equal(t, "ES256", got.TokenEndpointAuthSigner)
	require.True(t, got.DpopBoundAccessTokens)
	require.Equal(t, "atproto", got.Scope)
	require.Contains(t, got.GrantTypes, "urn:ietf:params:oauth:grant-type:jwt-bearer")
	require.NotNil(t, got.Jwks)
	require.Len(t, got.Jwks.Keys, 1)
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
