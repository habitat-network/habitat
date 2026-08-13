package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/httpx"
	logintestutil "github.com/habitat-network/habitat/internal/login/testutil"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/org"
	orgtestutil "github.com/habitat-network/habitat/internal/org/testutil"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/pkg/oauthclient"
	"github.com/habitat-network/habitat/pkg/sap"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

// jwtBearerIssuer is the fake public-looking hostname the JWT-bearer subject's
// PDS is registered under; jwtBearerRoundTripper rewrites requests to it onto
// a local httptest server, since indigo's oauth resolver rejects non-https,
// port-bearing, or non-public-looking URLs outright.
const jwtBearerIssuer = "https://habitat.example"

type jwtBearerRoundTripper struct {
	t      *testing.T
	server *httptest.Server
}

func (rt *jwtBearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Hostname() == "habitat.example" {
		u, err := url.Parse(rt.server.URL + req.URL.RequestURI())
		require.NoError(rt.t, err)
		req.URL = u
	}
	return rt.server.Client().Transport.RoundTrip(req)
}

// newJWTBearerAuthServer wires a minimal internal/oauthserver.OAuthServer
// approving clientID for the RFC 7523 JWT-bearer grant, exposing the
// well-known metadata and token endpoints SendJWTTokenRequest needs.
func newJWTBearerAuthServer(t *testing.T, clientID string) *httptest.Server {
	t.Helper()
	db := testutil.NewDB(t)
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)
	bytes, err := encrypt.ParseKey(secret)
	require.NoError(t, err)
	dummyDir := pdsclient.NewDummyDirectory("http://pds.url")

	orgStore := orgtestutil.NewTestStore(t)
	_, _, err = orgStore.CreateOrg(
		t.Context(), "org-name", "admin", "password", "", "", "", "contact@example.com")
	require.NoError(t, err)

	var oauthServer *oauthserver.OAuthServer
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			oauthServer.HandleToken(w, r)
		case "/.well-known/oauth-authorization-server":
			oauthServer.HandleAuthServerMetadata(w, r)
		case "/.well-known/oauth-protected-resource":
			oauthServer.HandleProtectedResourceMetadata(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	oauthServer, err = oauthserver.NewOAuthServer(
		bytes,
		&org.LoginRouter{Pds: logintestutil.NewPassthroughProvider(t)},
		dummyDir,
		db,
		noop.Meter{},
		orgStore,
		jwtBearerIssuer,
		oauthserver.NewJWTBearerStore(clientID),
	)
	require.NoError(t, err)

	return server
}

// fakeDirectory always fails to resolve: most tests below only exercise
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

	s, err := sap.New(sap.Config{DB: db, OAuthClient: oauthApp})
	require.NoError(t, err)

	return NewSapServer(s, jwtClient, "sap.example", nil)
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

// newJWTBearerTestServer wires a sap server whose oauth client's client ID
// resolves to a real (locally-served) client-metadata document — unlike
// newTestServer's fixed "https://sap.example" client ID, this is required
// here because the auth server verifies the JWT-bearer assertion's signature
// by actually fetching the client's metadata/JWKS over the network.
func newJWTBearerTestServer(t *testing.T) *server {
	t.Helper()

	db := testutil.NewDB(t)
	store, err := oauthclient.NewGormStore(db)
	require.NoError(t, err)

	var oauthApp *oauth.ClientApp
	metadataServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/client-metadata.json" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			metadata := oauthApp.Config.ClientMetadata()
			metadata.GrantTypes = append(metadata.GrantTypes, oauthclient.JWTBearerGrantType)
			jwks := oauthApp.Config.PublicJWKS()
			metadata.JWKS = &jwks
			httpx.WriteJSON(r.Context(), w, metadata)
		}),
	)
	t.Cleanup(metadataServer.Close)

	privateKey, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	keyID := "test-key"
	oauthApp = oauth.NewClientApp(&oauth.ClientConfig{
		ClientID:   metadataServer.URL + "/client-metadata.json",
		PrivateKey: privateKey,
		KeyID:      &keyID,
	}, store)
	jwtClient := oauthclient.NewJWTBearerClient(oauthApp)

	s, err := sap.New(sap.Config{DB: db, OAuthClient: oauthApp})
	require.NoError(t, err)

	return NewSapServer(s, jwtClient, "sap.example", nil)
}

// TestHandleAddJWTSessionAddsSession verifies the full JWT-bearer path:
// handleAddJWTSession mints a session via the RFC 7523 grant and hands its
// session ID to sap.AddSession, exactly as a completed browser OAuth
// callback would with its own session ID.
func TestHandleAddJWTSessionAddsSession(t *testing.T) {
	t.Parallel()

	srv := newJWTBearerTestServer(t)
	subject := syntax.DID("did:web:member.example")
	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID: subject,
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: jwtBearerIssuer},
		},
	})
	srv.oauthClient.Dir = dir

	authServer := newJWTBearerAuthServer(t, srv.oauthClient.Config.ClientID)
	rt := &jwtBearerRoundTripper{t: t, server: authServer}
	srv.oauthClient.Client = &http.Client{Transport: rt}
	srv.oauthClient.Resolver.Client = &http.Client{Transport: rt}

	body := strings.NewReader(`{"did":"` + subject.String() + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/session/jwt", body)
	w := httptest.NewRecorder()
	srv.handleAddJWTSession(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	sessions, err := srv.sap.Sessions(t.Context())
	require.NoError(t, err)
	require.Contains(t, sessions, subject)
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

func TestHandleAddJWTSessionSurfacesMintFailure(t *testing.T) {
	t.Parallel()

	// newTestServer's oauthClient.Dir (fakeDirectory) fails every lookup, so
	// minting the JWT-bearer session fails before AddSession is ever called.
	srv := newTestServer(t)
	req := httptest.NewRequest(
		http.MethodPost,
		"/session/jwt",
		strings.NewReader(`{"did":"did:web:member.example"}`),
	)
	w := httptest.NewRecorder()
	srv.handleAddJWTSession(w, req)
	require.Equal(t, http.StatusBadGateway, w.Code)
}
