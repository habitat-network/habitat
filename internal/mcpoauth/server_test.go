package mcpoauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	dbtestutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/stretchr/testify/require"
)

const (
	testOrigin   = "https://habitat.example"
	testRedirect = "http://127.0.0.1:9/callback"
	testVerifier = "test-verifier-test-verifier-test-verifier-1234567"
	testDID      = syntax.DID("did:web:alice.example")
)

// fakeBroker is a Broker fake standing in for the atproto login: Start hands
// back a fixed state, and Finish resolves it to testDID unless the callback
// carries an error.
type fakeBroker struct {
	identifiers []string
	failStart   bool
}

func (b *fakeBroker) Start(_ context.Context, identifier string) (string, string, error) {
	if b.failStart {
		return "", "", errors.New("no such account")
	}
	b.identifiers = append(b.identifiers, identifier)
	return "https://pds.example/authorize?request_uri=urn:x", "atproto-state-1", nil
}

func (b *fakeBroker) Finish(_ context.Context, q url.Values) (Login, error) {
	if q.Get("error") != "" {
		return Login{}, ErrLoginDenied
	}
	return Login{DID: testDID, State: q.Get("state")}, nil
}

type testServer struct {
	*Server
	http   *httptest.Server
	client *http.Client
	broker *fakeBroker
}

func setupTest(t *testing.T) *testServer {
	t.Helper()
	key, err := encrypt.GenerateKey()
	require.NoError(t, err)
	secret, err := encrypt.ParseKey(key)
	require.NoError(t, err)
	broker := &fakeBroker{}
	srv, err := New(secret, dbtestutil.NewDB(t), broker, oauth.ClientMetadata{}, testOrigin)
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc(MetadataPath, srv.HandleMetadata)
	mux.HandleFunc(RegisterPath, srv.HandleRegister)
	mux.HandleFunc(AuthorizePath, srv.HandleAuthorize)
	mux.HandleFunc(AuthorizeSubmitPath, srv.HandleAuthorizeSubmit)
	mux.HandleFunc(CallbackPath, srv.HandleCallback)
	mux.HandleFunc(TokenPath, srv.HandleToken)
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)
	return &testServer{
		Server: srv,
		http:   httpServer,
		broker: broker,
		client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
}

func (ts *testServer) register(t *testing.T) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"redirect_uris": []string{testRedirect}, "client_name": "Test MCP"})
	require.NoError(t, err)
	resp, err := ts.client.Post(ts.http.URL+RegisterPath, "application/json", bytes.NewReader(body))
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

func authorizeQuery(clientID string) url.Values {
	challenge := sha256.Sum256([]byte(testVerifier))
	return url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {testRedirect},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
		"state":                 {"client-state-123"},
		"resource":              {testOrigin + ResourcePath},
	}
}

// startAuthorize drives GET /authorize (the request validation step) and
// returns the request id the pear-pages handle prompt (AuthorizePagePath)
// would have received in its redirect's query string.
func (ts *testServer) startAuthorize(t *testing.T, clientID string) string {
	t.Helper()
	resp, err := ts.client.Get(ts.http.URL + AuthorizePath + "?" + authorizeQuery(clientID).Encode())
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	loc, err := resp.Location()
	require.NoError(t, err)
	require.Equal(t, AuthorizePagePath, loc.Path)
	requestID := loc.Query().Get("request_id")
	require.NotEmpty(t, requestID)
	return requestID
}

// submitHandle drives the JSON request the handle prompt sends once the user
// submits it, mirroring what typescript/apps/pear-pages/src/routes/login/mcp.tsx
// does.
func (ts *testServer) submitHandle(t *testing.T, requestID, handle string) (int, map[string]string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"requestId": requestID, "handle": handle})
	require.NoError(t, err)
	resp, err := ts.client.Post(ts.http.URL+AuthorizeSubmitPath, "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var out map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return resp.StatusCode, out
}

// authorize drives the full pre-PDS flow: request validation, then handle
// submission, returning once the browser would be sent to the PDS.
func (ts *testServer) authorize(t *testing.T, clientID string) {
	t.Helper()
	requestID := ts.startAuthorize(t, clientID)
	status, out := ts.submitHandle(t, requestID, "@alice.example")
	require.Equal(t, http.StatusOK, status, out)
	require.Equal(t, "https://pds.example/authorize?request_uri=urn:x", out["redirect"])
}

func (ts *testServer) callback(t *testing.T, q url.Values) *http.Response {
	t.Helper()
	resp, err := ts.client.Get(ts.http.URL + CallbackPath + "?" + q.Encode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func (ts *testServer) token(t *testing.T, form url.Values) (int, map[string]any) {
	t.Helper()
	resp, err := ts.client.PostForm(ts.http.URL+TokenPath, form)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return resp.StatusCode, out
}

func TestMCPOAuthFullFlow(t *testing.T) {
	ts := setupTest(t)
	clientID := ts.register(t)
	ts.authorize(t, clientID)
	require.Equal(t, []string{"alice.example"}, ts.broker.identifiers, "leading @ is trimmed")

	resp := ts.callback(t, url.Values{"state": {"atproto-state-1"}, "code": {"pds-code"}, "iss": {"https://pds.example"}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	loc, err := resp.Location()
	require.NoError(t, err)
	require.Equal(t, testRedirect, loc.Scheme+"://"+loc.Host+loc.Path)
	require.Equal(t, "client-state-123", loc.Query().Get("state"))
	require.Equal(t, ts.Issuer(), loc.Query().Get("iss"))
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)

	status, tok := ts.token(t, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID},
		"redirect_uri": {testRedirect}, "code_verifier": {testVerifier},
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
	require.Equal(t, testDID, cred.Subject)

	t.Run("refresh token grant issues a valid token", func(t *testing.T) {
		status, tok := ts.token(t, url.Values{
			"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {clientID},
		})
		require.Equal(t, http.StatusOK, status, tok)
		cred, ok, err := ts.ValidateRaw(t.Context(), tok["access_token"].(string))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, testDID, cred.Subject)
	})

	t.Run("authorization code is single use", func(t *testing.T) {
		status, _ := ts.token(t, url.Values{
			"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID},
			"redirect_uri": {testRedirect}, "code_verifier": {testVerifier},
		})
		require.NotEqual(t, http.StatusOK, status)
	})

	t.Run("garbage token is rejected", func(t *testing.T) {
		_, ok, err := ts.ValidateRaw(t.Context(), "not-a-token")
		require.Error(t, err)
		require.False(t, ok)
	})
}

func TestMCPOAuthWrongPKCEVerifierRejected(t *testing.T) {
	ts := setupTest(t)
	clientID := ts.register(t)
	ts.authorize(t, clientID)
	loc, err := ts.callback(t, url.Values{"state": {"atproto-state-1"}, "code": {"c"}, "iss": {"x"}}).Location()
	require.NoError(t, err)
	status, _ := ts.token(t, url.Values{
		"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "client_id": {clientID},
		"redirect_uri": {testRedirect}, "code_verifier": {"a-different-verifier-a-different-verifier-1234567"},
	})
	require.NotEqual(t, http.StatusOK, status)
}

func TestMCPOAuthDeniedLoginRedirectsWithError(t *testing.T) {
	ts := setupTest(t)
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
func (ts *testServer) authorizeOutcome(t *testing.T, q url.Values) (toPage bool, errorCode string) {
	t.Helper()
	resp, err := ts.client.Get(ts.http.URL + AuthorizePath + "?" + q.Encode())
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	if resp.StatusCode != http.StatusSeeOther {
		return false, ""
	}
	loc, err := resp.Location()
	require.NoError(t, err)
	if loc.Path == AuthorizePagePath {
		return true, ""
	}
	return false, loc.Query().Get("error")
}

func TestMCPOAuthAuthorizeValidation(t *testing.T) {
	ts := setupTest(t)
	clientID := ts.register(t)

	check := func(mutate func(url.Values)) (bool, string) {
		q := authorizeQuery(clientID)
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
	resp, err := ts.client.Get(ts.http.URL + AuthorizePath + "?" + func() string {
		q := authorizeQuery(clientID)
		q.Set("redirect_uri", "http://127.0.0.1:9/other")
		return q.Encode()
	}())
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.NotEqual(t, http.StatusSeeOther, resp.StatusCode)

	resp, err = ts.client.Get(ts.http.URL + AuthorizePath + "?" + func() string {
		q := authorizeQuery(clientID)
		q.Set("client_id", "mcp-unknown")
		return q.Encode()
	}())
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.NotEqual(t, http.StatusSeeOther, resp.StatusCode)
}

func TestMCPOAuthHandlePromptErrors(t *testing.T) {
	ts := setupTest(t)
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
	ts := setupTest(t)
	for name, body := range map[string]map[string]any{
		"no redirect uris":       {},
		"non-loopback http":      {"redirect_uris": []string{"http://evil.example/cb"}},
		"javascript scheme":      {"redirect_uris": []string{"javascript:alert(1)"}},
		"unsupported grant type": {"redirect_uris": []string{testRedirect}, "grant_types": []string{"password"}},
	} {
		t.Run(name, func(t *testing.T) {
			b, err := json.Marshal(body)
			require.NoError(t, err)
			resp, err := ts.client.Post(ts.http.URL+RegisterPath, "application/json", bytes.NewReader(b))
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
	ts := setupTest(t)
	resp, err := ts.client.Get(ts.http.URL + MetadataPath)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var md map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&md))
	require.Equal(t, testOrigin+"/mcp", md["issuer"])
	require.Equal(t, testOrigin+RegisterPath, md["registration_endpoint"])
	require.Equal(t, testOrigin+AuthorizePath, md["authorization_endpoint"])
	require.NotContains(t, md, "scopes_supported", "clients shouldn't be told to request a scope")
	require.False(t, strings.Contains(string(mustJSON(t, md)), "client_id_metadata_document"))
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
