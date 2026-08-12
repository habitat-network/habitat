package oauthserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/did"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/httpx"
	login_testutil "github.com/habitat-network/habitat/internal/login/testutil"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/pkg/oauthclient"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

const jwtBearerGrantType = "urn:ietf:params:oauth:grant-type:jwt-bearer"

// jwtBearerIssuer is the fixed, public-looking issuer URL every JWT Bearer
// test server issues from. indigo's oauth.Resolver refuses to resolve auth
// server / protected-resource metadata from anything but an https URL with no
// port, so tests can't point SendJWTTokenRequest's identity/auth-server
// resolution directly at an httptest server's http://127.0.0.1:PORT address.
// Instead, every request to this host is rewritten to the real httptest.Server
// by roundTripper (defined in oauth_server_test.go).
const jwtBearerIssuer = "https://habitat.example"

// newJWTBearerTestClient creates an httptest server serving the client's
// metadata document, and returns the client wired up to it.
func newJWTBearerTestClient(t *testing.T) *oauthclient.Client {
	t.Helper()
	privateKey, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	var client *oauth.ClientApp

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client-metadata.json":
			metadata := client.Config.ClientMetadata()
			metadata.GrantTypes = append(metadata.GrantTypes, jwtBearerGrantType)
			jwks := client.Config.PublicJWKS()
			metadata.JWKS = &jwks
			httpx.WriteJSON(r.Context(), w, metadata)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	client = oauth.NewClientApp(
		&oauth.ClientConfig{
			ClientID:   server.URL + "/client-metadata.json",
			PrivateKey: privateKey,
			KeyID:      new("test-key"),
		},
		oauth.NewMemStore(),
	)

	return oauthclient.NewJWTBearerClient(client)
}

// setupJWTBearerTestServer wires up an OAuthServer issuing from
// jwtBearerIssuer, reachable through a TLS httptest.Server. approvedClientIDs
// are registered in the JWT Bearer client allow-list. Callers must route the
// oauthclient.Client's HTTP traffic to the returned server via roundTripper
// (see jwtBearerIssuer) before calling SendJWTTokenRequest.
func setupJWTBearerTestServer(
	t *testing.T,
	approvedClientIDs ...string,
) (srv *OAuthServer, server *httptest.Server) {
	t.Helper()
	db := testutil.NewDB(t)
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)
	bytes, err := encrypt.ParseKey(secret)
	require.NoError(t, err)
	dummyDir := pdsclient.NewDummyDirectory("http://pds.url")

	var oauthServer *OAuthServer
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	oauthServer, err = NewOAuthServer(
		bytes,
		&org.LoginRouter{Pds: login_testutil.NewPassthroughProvider(t)},
		dummyDir,
		db,
		noop.Meter{},
		testStore(t),
		jwtBearerIssuer,
		NewJWTBearerStore(approvedClientIDs...),
	)
	require.NoError(t, err)

	return oauthServer, server
}

func TestHandleTokenJWTBearerGrant(t *testing.T) {
	subject := syntax.DID("did:web:service-subject.example")
	dir := identity.NewMockDirectory()
	dir.Insert(*did.New(subject).ATProtoPDS(jwtBearerIssuer).Build())
	client := newJWTBearerTestClient(t)
	client.Dir = dir

	t.Run("issues an access token for an allow-listed client", func(t *testing.T) {
		srv, server := setupJWTBearerTestServer(t, client.Config.ClientID)
		rt := &roundTripper{t: t, server: server}
		client.Client = &http.Client{Transport: rt}
		client.Resolver.Client = &http.Client{Transport: rt}

		sess, err := client.SendJWTTokenRequest(t.Context(), subject.String())
		require.NoError(t, err)
		require.NotEmpty(t, sess.AccessToken)
		require.Equal(t, subject, sess.AccountDID)

		credInfo, ok, err := srv.ValidateRaw(t.Context(), sess.AccessToken)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, subject, credInfo.Subject)
	})

	t.Run("rejects an assertion from a client not on the allow-list", func(t *testing.T) {
		_, server := setupJWTBearerTestServer(t)
		rt := &roundTripper{t: t, server: server}
		client.Client = &http.Client{Transport: rt}
		client.Resolver.Client = &http.Client{Transport: rt}

		_, err := client.SendJWTTokenRequest(t.Context(), subject.String())
		require.Error(t, err)
	})
}
