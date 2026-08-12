package oauthclient

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/httpx"
	login_testutil "github.com/habitat-network/habitat/internal/login/testutil"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/org"
	org_testutil "github.com/habitat-network/habitat/internal/org/testutil"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

const jwtBearerGrantType = "urn:ietf:params:oauth:grant-type:jwt-bearer"

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

// jwtBearerTestOrgStore creates an org.Store seeded with a test org.
func jwtBearerTestOrgStore(t *testing.T) org.Store {
	t.Helper()
	s := org_testutil.NewTestStore(t)
	_, _, err := s.CreateOrg(
		t.Context(),
		"org-name",
		"admin",
		"password",
		"",
		"",
		"",
		"contact@example.com",
	)
	require.NoError(t, err)
	return s
}

// newJWTBearerTestClient creates an httptest server serving the client's
// metadata document, and returns the oauthclient.Client wired up to it.
func newJWTBearerTestClient(t *testing.T) *Client {
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
	keyID := "test-key"
	client = oauth.NewClientApp(
		&oauth.ClientConfig{
			ClientID:   server.URL + "/client-metadata.json",
			PrivateKey: privateKey,
			KeyID:      &keyID,
		},
		oauth.NewMemStore(),
	)

	return NewJWTBearerClient(client)
}

func setupJWTBearerTestServer(
	t *testing.T,
	approvedClientIDs ...string,
) (srv *oauthserver.OAuthServer, server *httptest.Server) {
	t.Helper()
	db := testutil.NewDB(t)
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)
	bytes, err := encrypt.ParseKey(secret)
	require.NoError(t, err)
	dummyDir := pdsclient.NewDummyDirectory("http://pds.url")

	var oauthServer *oauthserver.OAuthServer
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

	oauthServer, err = oauthserver.NewOAuthServer(
		bytes,
		&org.LoginRouter{Pds: login_testutil.NewPassthroughProvider(t)},
		dummyDir,
		db,
		noop.Meter{},
		jwtBearerTestOrgStore(t),
		jwtBearerIssuer,
		oauthserver.NewJWTBearerStore(approvedClientIDs...),
	)
	require.NoError(t, err)

	return oauthServer, server
}

func TestClient_SendJWTTokenRequest(t *testing.T) {
	subject := syntax.DID("did:web:service-subject.example")
	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID: subject,
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: jwtBearerIssuer},
		},
	})
	client := newJWTBearerTestClient(t)
	client.Dir = dir

	t.Run("signs and exchanges an assertion for a session", func(t *testing.T) {
		srv, server := setupJWTBearerTestServer(t, client.Config.ClientID)
		rt := &jwtBearerRoundTripper{t: t, server: server}
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

		stored, err := client.Store.GetSession(t.Context(), subject, sess.SessionID)
		require.NoError(t, err)
		require.Equal(t, sess.AccessToken, stored.AccessToken)
	})

	t.Run("rejects an assertion from a client not on the allow-list", func(t *testing.T) {
		_, server := setupJWTBearerTestServer(t)
		rt := &jwtBearerRoundTripper{t: t, server: server}
		client.Client = &http.Client{Transport: rt}
		client.Resolver.Client = &http.Client{Transport: rt}

		_, err := client.SendJWTTokenRequest(t.Context(), subject.String())
		require.Error(t, err)
	})

	t.Run("rejects a public (non-confidential) client", func(t *testing.T) {
		app := oauth.NewClientApp(&oauth.ClientConfig{
			ClientID: "https://client.example/client-metadata.json",
		}, oauth.NewMemStore())
		c := NewJWTBearerClient(app)

		_, err := c.SendJWTTokenRequest(t.Context(), subject.String())
		require.Error(t, err)
	})

	t.Run("fails when the identifier can't be resolved", func(t *testing.T) {
		_, server := setupJWTBearerTestServer(t, client.Config.ClientID)
		rt := &jwtBearerRoundTripper{t: t, server: server}
		c := newJWTBearerTestClient(t)
		c.Client = &http.Client{Transport: rt}
		c.Resolver.Client = &http.Client{Transport: rt}
		c.Dir = identity.NewMockDirectory() // subject not registered

		_, err := c.SendJWTTokenRequest(t.Context(), subject.String())
		require.Error(t, err)
	})
}
