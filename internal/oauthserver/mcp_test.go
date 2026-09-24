package oauthserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	dbtestutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	login_testutil "github.com/habitat-network/habitat/internal/login/testutil"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

const (
	mcpTestOrigin   = "https://habitat.example"
	mcpTestRedirect = "http://127.0.0.1:9/callback"
	mcpTestVerifier = "test-verifier-test-verifier-test-verifier-1234567"
	// mcpTestDID is the DID the dummy PDS always issues tokens for (see
	// pdsclient.DummyOAuthClient.ExchangeCode); the handle prompt is given
	// this DID directly as the "handle" to sign in with.
	mcpTestDID = "did:web:example.did.com"
)

// mcpTestServer wires an OAuthServer's MCP-shaped endpoints, plus the shared
// /oauth-callback and /oauth/token endpoints the MCP sign-in flow lands on,
// onto an httptest.Server, mirroring how cmd/pear/main.go mounts them.
type mcpTestServer struct {
	*OAuthServer
	http   *httptest.Server
	client *http.Client
}

func setupMCPTest(t *testing.T) *mcpTestServer {
	t.Helper()
	key, err := encrypt.GenerateKey()
	require.NoError(t, err)
	secret, err := encrypt.ParseKey(key)
	require.NoError(t, err)
	dummyDir := pdsclient.NewDummyDirectory("http://pds.url")
	pds := login_testutil.NewPassthroughProvider(t)
	srv, err := NewOAuthServer(
		secret,
		&org.LoginRouter{Pds: pds},
		dummyDir,
		dbtestutil.NewDB(t),
		noop.Meter{},
		testStore(t),
		mcpTestOrigin,
		NewJWTBearerStore(),
		testOpensocialStore(t),
		nil,
	)
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc(MCPMetadataPath, srv.HandleMCPMetadata)
	mux.HandleFunc(MCPRegisterPath, srv.HandleMCPRegister)
	mux.HandleFunc(MCPAuthorizePath, srv.HandleMCPAuthorize)
	mux.HandleFunc(MCPAuthorizeSubmitPath, srv.HandleMCPAuthorizeSubmit)
	mux.HandleFunc(MCPTokenPath, srv.HandleToken)
	mux.HandleFunc("/oauth-callback", srv.HandleCallback)
	httpServer := httptest.NewTLSServer(mux)
	t.Cleanup(httpServer.Close)
	pds.RedirectURI = httpServer.URL + "/oauth-callback"

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := httpServer.Client()
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &mcpTestServer{
		OAuthServer: srv,
		http:        httpServer,
		client:      client,
	}
}

func (ts *mcpTestServer) register(t *testing.T) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"redirect_uris": []string{mcpTestRedirect}, "client_name": "Test MCP"})
	require.NoError(t, err)
	resp, err := ts.client.Post(ts.http.URL+MCPRegisterPath, "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var out struct {
		ClientID string `json:"client_id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.NotEmpty(t, out.ClientID)
	return out.ClientID
}

func mcpAuthorizeQuery(origin, clientID string) url.Values {
	challenge := sha256.Sum256([]byte(mcpTestVerifier))
	return url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {mcpTestRedirect},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
		"state":                 {"client-state-123"},
		"resource":              {origin + MCPResourcePath},
	}
}

func (ts *mcpTestServer) startAuthorize(t *testing.T, clientID string) {
	t.Helper()
	resp, err := ts.client.Get(ts.http.URL + MCPAuthorizePath + "?" + mcpAuthorizeQuery(mcpTestOrigin, clientID).Encode())
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	loc, err := resp.Location()
	require.NoError(t, err)
	require.Equal(t, MCPAuthorizePagePath, loc.Path)
}

func (ts *mcpTestServer) submitHandle(t *testing.T, handle string) (int, map[string]string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"handle": handle})
	require.NoError(t, err)
	resp, err := ts.client.Post(ts.http.URL+MCPAuthorizeSubmitPath, "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var out map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return resp.StatusCode, out
}

// authorize drives the flow from the client's initial authorize request
// through the handle prompt and the (fake) PDS login, and returns the
// response that redirects back to the MCP client with an authorization code.
// The redirect HandleMCPAuthorizeSubmit returns points at the passthrough
// login provider's own test server, which immediately redirects again to
// this server's /oauth-callback — hence the two hops.
func (ts *mcpTestServer) authorize(t *testing.T, clientID string) *http.Response {
	t.Helper()
	ts.startAuthorize(t, clientID)
	status, out := ts.submitHandle(t, mcpTestDID)
	require.Equal(t, http.StatusOK, status, out)
	resp, err := ts.client.Get(out["redirect"])
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	loc, err := resp.Location()
	require.NoError(t, err)
	resp, err = ts.client.Get(loc.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func (ts *mcpTestServer) token(t *testing.T, form url.Values) (int, map[string]any) {
	t.Helper()
	resp, err := ts.client.PostForm(ts.http.URL+MCPTokenPath, form)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return resp.StatusCode, out
}

func TestMCPOAuthFullFlow(t *testing.T) {
	ts := setupMCPTest(t)
	clientID := ts.register(t)

	resp := ts.authorize(t, clientID)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	loc, err := resp.Location()
	require.NoError(t, err)
	require.Equal(t, mcpTestRedirect, loc.Scheme+"://"+loc.Host+loc.Path)
	require.Equal(t, "client-state-123", loc.Query().Get("state"))
	require.Equal(t, ts.MCPIssuer(), loc.Query().Get("iss"))
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)

	status, tok := ts.token(t, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID},
		"redirect_uri": {mcpTestRedirect}, "code_verifier": {mcpTestVerifier},
	})
	require.Equal(t, http.StatusOK, status, tok)
	require.Equal(t, "Bearer", tok["token_type"])
	access, _ := tok["access_token"].(string)
	refresh, _ := tok["refresh_token"].(string)
	require.NotEmpty(t, access)
	require.NotEmpty(t, refresh)

	cred, ok, err := ts.ValidateRaw(t.Context(), access)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, mcpTestDID, cred.Subject.String())

	t.Run("refresh token grant issues a valid token", func(t *testing.T) {
		status, tok := ts.token(t, url.Values{
			"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {clientID},
		})
		require.Equal(t, http.StatusOK, status, tok)
		cred, ok, err := ts.ValidateRaw(t.Context(), tok["access_token"].(string))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, mcpTestDID, cred.Subject.String())
	})

	t.Run("authorization code is single use", func(t *testing.T) {
		status, _ := ts.token(t, url.Values{
			"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID},
			"redirect_uri": {mcpTestRedirect}, "code_verifier": {mcpTestVerifier},
		})
		require.NotEqual(t, http.StatusOK, status)
	})
}

func TestMCPOAuthWrongPKCEVerifierRejected(t *testing.T) {
	ts := setupMCPTest(t)
	clientID := ts.register(t)
	resp := ts.authorize(t, clientID)
	loc, err := resp.Location()
	require.NoError(t, err)
	status, _ := ts.token(t, url.Values{
		"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "client_id": {clientID},
		"redirect_uri": {mcpTestRedirect}, "code_verifier": {"a-different-verifier-a-different-verifier-1234567"},
	})
	require.NotEqual(t, http.StatusOK, status)
}

// authorizeOutcome is either the pear-pages redirect for a valid request, or
// an OAuth error. Fosite reports most validation failures as a redirect back
// to the client's own redirect_uri with error query params (same status as
// success), so the two are told apart by where the redirect points, not by
// status code.
func (ts *mcpTestServer) authorizeOutcome(t *testing.T, q url.Values) (toPage bool, errorCode string) {
	t.Helper()
	resp, err := ts.client.Get(ts.http.URL + MCPAuthorizePath + "?" + q.Encode())
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	if resp.StatusCode != http.StatusSeeOther {
		return false, ""
	}
	loc, err := resp.Location()
	require.NoError(t, err)
	if loc.Path == MCPAuthorizePagePath {
		return true, ""
	}
	return false, loc.Query().Get("error")
}

func TestMCPOAuthAuthorizeValidation(t *testing.T) {
	ts := setupMCPTest(t)
	clientID := ts.register(t)

	check := func(mutate func(url.Values)) (bool, string) {
		q := mcpAuthorizeQuery(mcpTestOrigin, clientID)
		mutate(q)
		return ts.authorizeOutcome(t, q)
	}
	toPage, _ := check(func(url.Values) {})
	require.True(t, toPage)

	toPage, errCode := check(func(q url.Values) { q.Del("code_challenge") })
	require.False(t, toPage, "PKCE is required")
	require.Equal(t, "invalid_request", errCode)

	toPage, errCode = check(func(q url.Values) { q.Set("resource", "https://other.example/mcp") })
	require.False(t, toPage)
	require.Equal(t, "invalid_request", errCode)

	// An unregistered redirect_uri or client_id can't be redirected to at all.
	resp, err := ts.client.Get(ts.http.URL + MCPAuthorizePath + "?" + func() string {
		q := mcpAuthorizeQuery(mcpTestOrigin, clientID)
		q.Set("redirect_uri", "http://127.0.0.1:9/other")
		return q.Encode()
	}())
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.NotEqual(t, http.StatusSeeOther, resp.StatusCode)

	resp, err = ts.client.Get(ts.http.URL + MCPAuthorizePath + "?" + func() string {
		q := mcpAuthorizeQuery(mcpTestOrigin, clientID)
		q.Set("client_id", "mcp-unknown")
		return q.Encode()
	}())
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.NotEqual(t, http.StatusSeeOther, resp.StatusCode)
}

func TestMCPOAuthHandlePromptErrors(t *testing.T) {
	ts := setupMCPTest(t)
	ts.startAuthorize(t, ts.register(t))

	status, out := ts.submitHandle(t, "not a valid handle or did")
	require.Equal(t, http.StatusBadRequest, status)
	require.Contains(t, out["message"], "sign in with that handle")

	status, _ = ts.submitHandle(t, "")
	require.Equal(t, http.StatusBadRequest, status, "an empty handle is rejected")
}

func TestMCPOAuthRegisterValidation(t *testing.T) {
	ts := setupMCPTest(t)
	for name, body := range map[string]map[string]any{
		"no redirect uris":       {},
		"non-loopback http":      {"redirect_uris": []string{"http://evil.example/cb"}},
		"javascript scheme":      {"redirect_uris": []string{"javascript:alert(1)"}},
		"unsupported grant type": {"redirect_uris": []string{mcpTestRedirect}, "grant_types": []string{"password"}},
	} {
		t.Run(name, func(t *testing.T) {
			b, err := json.Marshal(body)
			require.NoError(t, err)
			resp, err := ts.client.Post(ts.http.URL+MCPRegisterPath, "application/json", bytes.NewReader(b))
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	}
	// Native app custom schemes and https are fine.
	for _, uri := range []string{"cursor://anysphere.cursor/callback", "https://claude.ai/api/mcp/auth_callback"} {
		require.NoError(t, validateRedirectURI(uri))
	}
}

func TestMCPOAuthMetadata(t *testing.T) {
	ts := setupMCPTest(t)
	resp, err := ts.client.Get(ts.http.URL + MCPMetadataPath)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var md map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&md))
	require.Equal(t, mcpTestOrigin+"/mcp", md["issuer"])
	require.Equal(t, mcpTestOrigin+MCPRegisterPath, md["registration_endpoint"])
	require.Equal(t, mcpTestOrigin+MCPAuthorizePath, md["authorization_endpoint"])
	require.NotContains(t, md, "scopes_supported", "clients shouldn't be told to request a scope")
}
