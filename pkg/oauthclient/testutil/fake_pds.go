// Package testutil provides a fake atproto PDS / OAuth authorization server
// for tests that exercise a real oauth.ClientApp's StartAuthFlow/
// ProcessCallback round trip.
//
// indigo's oauth.Resolver deliberately refuses to resolve loopback/private
// hosts or hosts with an explicit port (SSRF protection), so a plain
// httptest.Server can't be used as the "identifier"/auth-server URL
// directly. FakePDS works around this the same way as other tests in this
// repo: requests to a public-looking, port-less https:// origin are
// rewritten to the underlying local server via a custom http.RoundTripper.
package testutil

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

// FakeDID is the account DID FakePDS reports for a completed login.
const FakeDID = syntax.DID("did:web:example.did.com")

// FakePDS is a minimal fake atproto PDS / OAuth authorization server:
// discovery metadata, a PAR endpoint, an authorize endpoint that
// immediately "approves" the request with a canned code, and a token
// endpoint that reports FakeDID as the authenticated subject.
type FakePDS struct {
	// URL is the public-looking, port-less https:// origin this fixture
	// answers for — pass it as the loginHint/identifier to StartAuthFlow, or
	// use it to build a Directory entry pointing at this PDS.
	URL string
	// Client rewrites requests targeting URL to the underlying local server.
	// Assign it to both oauth.ClientApp.Client and
	// oauth.ClientApp.Resolver.Client (see [FakePDS.Configure]).
	Client *http.Client

	server       *httptest.Server
	lastState    string
	lastRedirect string
}

// NewFakePDS starts a FakePDS answering for host, a public-looking, port-less
// https:// origin (e.g. "https://pds.example.com").
func NewFakePDS(t *testing.T, host string) *FakePDS {
	t.Helper()
	f := &FakePDS{URL: host}

	handleProtectedResource := func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authorization_servers": []string{host},
		})
	}
	handleAuthServerMetadata := func(w http.ResponseWriter, r *http.Request) {
		meta := oauth.AuthServerMetadata{}
		meta.Issuer = host
		meta.AuthorizationEndpoint = host + "/oauth/authorize"
		meta.TokenEndpoint = host + "/oauth/token"
		meta.ResponseTypesSupported = []string{"code"}
		meta.GrantTypesSupported = []string{"authorization_code", "refresh_token"}
		meta.CodeChallengeMethodsSupported = []string{"S256"}
		meta.TokenEndpointAuthMethodsSupoorted = []string{"none", "private_key_jwt"}
		meta.TokenEndpointAuthSigningAlgValuesSupported = []string{"ES256"}
		meta.ScopesSupported = []string{"atproto", "transition:generic"}
		meta.AuthorizationReponseISSParameterSupported = true
		meta.RequirePushedAuthorizationRequests = true
		meta.PushedAuthorizationRequestEndpoint = host + "/oauth/par"
		meta.DPoPSigningAlgValuesSupported = []string{"ES256"}
		meta.ClientIDMetadataDocumentSupported = true
		_ = json.NewEncoder(w).Encode(meta)
	}
	handlePAR := func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		f.lastState = r.PostForm.Get("state")
		f.lastRedirect = r.PostForm.Get("redirect_uri")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"request_uri": "urn:ietf:params:oauth:request_uri:fake",
			"expires_in":  60,
		})
	}
	handleAuthorize := func(w http.ResponseWriter, r *http.Request) {
		q := url.Values{
			"code":  {"dummyCode"},
			"iss":   {host},
			"state": {f.lastState},
		}
		http.Redirect(w, r, f.lastRedirect+"?"+q.Encode(), http.StatusSeeOther)
	}
	handleToken := func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, "dummyCode", r.PostForm.Get("code"))
		_ = json.NewEncoder(w).Encode(oauth.TokenResponse{
			Subject:      FakeDID.String(),
			Scope:        "atproto transition:generic",
			AccessToken:  "fake-access-token",
			RefreshToken: "fake-refresh-token",
		})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-protected-resource", handleProtectedResource)
	mux.HandleFunc("/.well-known/oauth-authorization-server", handleAuthServerMetadata)
	mux.HandleFunc("/oauth/par", handlePAR)
	mux.HandleFunc("/oauth/authorize", handleAuthorize)
	mux.HandleFunc("/oauth/token", handleToken)

	f.server = httptest.NewTLSServer(mux)
	t.Cleanup(f.server.Close)

	targetHost := strings.TrimPrefix(f.server.URL, "https://")
	//nolint:gosec // test-only fixture talking to its own local httptest server
	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	f.Client = &http.Client{Transport: &rewriteTransport{
		targetHost: targetHost,
		inner:      &http.Transport{TLSClientConfig: tlsConfig},
	}}
	return f
}

// Configure points app at this fixture: it resolves FakeDID to a PDS hosted
// at URL, and rewrites app's outbound requests to this fixture's server.
func (f *FakePDS) Configure(app *oauth.ClientApp) {
	app.Client = f.Client
	app.Resolver.Client = f.Client
	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID: FakeDID,
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {URL: f.URL},
		},
	})
	app.Dir = dir
}

// rewriteTransport routes requests targeting a public-looking host to the
// underlying local test server, leaving requests already addressed to the
// test server (loopback) alone.
type rewriteTransport struct {
	targetHost string
	inner      *http.Transport
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !strings.HasPrefix(req.URL.Host, "127.0.0.1") {
		req = req.Clone(req.Context())
		req.URL.Host = rt.targetHost
	}
	return rt.inner.RoundTrip(req)
}
