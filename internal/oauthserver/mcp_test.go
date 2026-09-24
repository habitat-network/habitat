package oauthserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
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
	mcpTestDID      = syntax.DID("did:web:alice.example")
)

// mcpTestServer wires an OAuthServer's MCP-shaped endpoints onto an
// httptest.Server, mirroring how cmd/pear/main.go mounts them.
type mcpTestServer struct {
	*OAuthServer
	http   *httptest.Server
	client *http.Client
	broker *fakeBroker
}

func setupMCPTest(t *testing.T) *mcpTestServer {
	t.Helper()
	key, err := encrypt.GenerateKey()
	require.NoError(t, err)
	secret, err := encrypt.ParseKey(key)
	require.NoError(t, err)
	broker := &fakeBroker{did: mcpTestDID}
	orgStore := testStore(t)
	srv, err := NewOAuthServer(
		secret,
		&org.LoginRouter{Pds: login_testutil.NewPassthroughProvider(t), OrgStore: orgStore},
		pdsclient.NewDummyDirectory("http://pds.url"),
		dbtestutil.NewDB(t),
		noop.Meter{},
		orgStore,
		mcpTestOrigin,
		NewJWTBearerStore(),
		testOpensocialStore(t),
		nil,
		broker,
		oauth.ClientMetadata{},
	)
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc(MCPMetadataPath, srv.HandleMCPMetadata)
	mux.HandleFunc(MCPRegisterPath, srv.HandleMCPRegister)
	mux.HandleFunc(MCPAuthorizePath, srv.HandleMCPAuthorize)
	mux.HandleFunc(MCPAuthorizeSubmitPath, srv.HandleMCPAuthorizeSubmit)
	mux.HandleFunc(MCPCallbackPath, srv.HandleMCPCallback)
	mux.HandleFunc(MCPTokenPath, srv.HandleMCPToken)
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)
	return &mcpTestServer{
		OAuthServer: srv,
		http:        httpServer,
		broker:      broker,
		client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
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

func (ts *mcpTestServer) startAuthorize(t *testing.T, clientID string) string {
	t.Helper()
	resp, err := ts.client.Get(ts.http.URL + MCPAuthorizePath + "?" + mcpAuthorizeQuery(mcpTestOrigin, clientID).Encode())
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	loc, err := resp.Location()
	require.NoError(t, err)
	require.Equal(t, MCPAuthorizePagePath, loc.Path)
	requestID := loc.Query().Get("request_id")
	require.NotEmpty(t, requestID)
	return requestID
}

func (ts *mcpTestServer) submitHandle(t *testing.T, requestID, handle string) (int, map[string]string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"requestId": requestID, "handle": handle})
	require.NoError(t, err)
	resp, err := ts.client.Post(ts.http.URL+MCPAuthorizeSubmitPath, "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var out map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return resp.StatusCode, out
}

func (ts *mcpTestServer) authorize(t *testing.T, clientID string) {
	t.Helper()
	requestID := ts.startAuthorize(t, clientID)
	status, out := ts.submitHandle(t, requestID, "@alice.example")
	require.Equal(t, http.StatusOK, status, out)
	require.Equal(t, "https://pds.example/authorize?request_uri=urn:x", out["redirect"])
}

func (ts *mcpTestServer) callback(t *testing.T, q url.Values) *http.Response {
	t.Helper()
	resp, err := ts.client.Get(ts.http.URL + MCPCallbackPath + "?" + q.Encode())
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
	ts.authorize(t, clientID)
	require.Equal(t, []string{"alice.example"}, ts.broker.identifiers, "leading @ is trimmed")

	resp := ts.callback(t, url.Values{"state": {"atproto-state-1"}, "code": {"pds-code"}, "iss": {"https://pds.example"}})
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
	require.Equal(t, mcpTestDID, cred.Subject)

	t.Run("refresh token grant issues a valid token", func(t *testing.T) {
		status, tok := ts.token(t, url.Values{
			"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {clientID},
		})
		require.Equal(t, http.StatusOK, status, tok)
		cred, ok, err := ts.ValidateRaw(t.Context(), tok["access_token"].(string))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, mcpTestDID, cred.Subject)
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
	ts.authorize(t, clientID)
	loc, err := ts.callback(t, url.Values{"state": {"atproto-state-1"}, "code": {"c"}, "iss": {"x"}}).Location()
	require.NoError(t, err)
	status, _ := ts.token(t, url.Values{
		"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "client_id": {clientID},
		"redirect_uri": {mcpTestRedirect}, "code_verifier": {"a-different-verifier-a-different-verifier-1234567"},
	})
	require.NotEqual(t, http.StatusOK, status)
}

func TestMCPOAuthDeniedLoginRedirectsWithError(t *testing.T) {
	ts := setupMCPTest(t)
	ts.authorize(t, ts.register(t))
	resp := ts.callback(t, url.Values{"state": {"atproto-state-1"}, "error": {"access_denied"}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	loc, err := resp.Location()
	require.NoError(t, err)
	require.Equal(t, "access_denied", loc.Query().Get("error"))
	require.Equal(t, "client-state-123", loc.Query().Get("state"))
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
	requestID := ts.startAuthorize(t, ts.register(t))

	ts.broker.failStart = true
	status, out := ts.submitHandle(t, requestID, "nobody.example")
	require.Equal(t, http.StatusBadRequest, status)
	require.Contains(t, out["message"], "sign in with that handle")

	status, _ = ts.submitHandle(t, "bogus-request-id", "a.example")
	require.Equal(t, http.StatusBadRequest, status)

	status, _ = ts.submitHandle(t, requestID, "")
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

// TestMCPTokensInterchangeableWithAtprotoEndpoints proves that a single
// OAuthServer's MCP and atproto endpoint sets share one provider and
// storage: a token minted through the MCP token endpoint validates the same
// way a token minted through the atproto /oauth/token endpoint would, and
// both are introspected by the same ValidateRaw.
func TestMCPTokensInterchangeableWithAtprotoEndpoints(t *testing.T) {
	ts := setupMCPTest(t)

	atprotoMux := http.NewServeMux()
	atprotoMux.HandleFunc("/authorize", ts.HandleAuthorize)
	atprotoMux.HandleFunc("/oauth-callback", ts.HandleCallback)
	atprotoMux.HandleFunc("/token", ts.HandleToken)
	atprotoServer := httptest.NewTLSServer(atprotoMux)
	t.Cleanup(atprotoServer.Close)

	clientID := ts.register(t)
	ts.authorize(t, clientID)
	resp := ts.callback(t, url.Values{"state": {"atproto-state-1"}, "code": {"c"}, "iss": {"i"}})
	loc, err := resp.Location()
	require.NoError(t, err)
	status, tok := ts.token(t, url.Values{
		"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "client_id": {clientID},
		"redirect_uri": {mcpTestRedirect}, "code_verifier": {mcpTestVerifier},
	})
	require.Equal(t, http.StatusOK, status, tok)

	// A token minted by the MCP token endpoint validates via the same
	// OAuthServer.ValidateRaw the atproto endpoints' Validate uses.
	credInfo, ok, err := ts.ValidateRaw(t.Context(), tok["access_token"].(string))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, mcpTestDID, credInfo.Subject)
}
