package oauthserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
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
	"golang.org/x/oauth2"
)

func TestHandleRegisterRejectsMissingRedirectURIs(t *testing.T) {
	db := dbtestutil.NewDB(t)
	secretBytes, err := encrypt.ParseKey(mustGenerateKey(t))
	require.NoError(t, err)
	oauthSrv, err := NewOAuthServer(
		secretBytes, &org.LoginRouter{Pds: login_testutil.NewPassthroughProvider(t)},
		pdsclient.NewDummyDirectory("http://pds.url"), db, noop.Meter{}, testStore(t),
		"https://habitat.example", NewJWTBearerStore(), testOpensocialStore(t),
	)
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodPost, "/oauth/register",
		bytes2Reader(t, map[string]any{}),
	)
	w := httptest.NewRecorder()
	oauthSrv.HandleRegister(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleRegisterIssuesUsableClient(t *testing.T) {
	db := dbtestutil.NewDB(t)
	secretBytes, err := encrypt.ParseKey(mustGenerateKey(t))
	require.NoError(t, err)
	oauthSrv, err := NewOAuthServer(
		secretBytes, &org.LoginRouter{Pds: login_testutil.NewPassthroughProvider(t)},
		pdsclient.NewDummyDirectory("http://pds.url"), db, noop.Meter{}, testStore(t),
		"https://habitat.example", NewJWTBearerStore(), testOpensocialStore(t),
	)
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodPost, "/oauth/register",
		bytes2Reader(t, map[string]any{
			"redirect_uris": []string{"http://client.example/callback"},
			"client_name":   "Test MCP Client",
		}),
	)
	w := httptest.NewRecorder()
	oauthSrv.HandleRegister(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp registerResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotEmpty(t, resp.ClientID)
	require.Equal(t, []string{"http://client.example/callback"}, resp.RedirectURIs)
	require.Equal(t, "none", resp.TokenEndpointAuthMethod)
	require.Equal(t, "Test MCP Client", resp.ClientName)

	// The registered client must be resolvable the same way any other client
	// is, since it now shares the authorize/token pipeline with atproto
	// Client ID Metadata Document clients.
	fositeClient, err := oauthSrv.storage.GetClient(context.Background(), resp.ClientID)
	require.NoError(t, err)
	require.Equal(t, resp.ClientID, fositeClient.GetID())
	require.True(t, fositeClient.IsPublic())
}

// TestDynamicClientRegistrationE2E drives the full authorization code flow
// for a client that only knows its client_id via Dynamic Client Registration
// (RFC 7591) — the path generic MCP clients use, since they can't publish an
// atproto Client ID Metadata Document the way TestOAuthServerE2E's client
// app does.
func TestDynamicClientRegistrationE2E(t *testing.T) {
	db := dbtestutil.NewDB(t)
	secretBytes, err := encrypt.ParseKey(mustGenerateKey(t))
	require.NoError(t, err)

	dummyDir := pdsclient.NewDummyDirectory("http://pds.url")
	pds := login_testutil.NewPassthroughProvider(t)
	oauthSrv, err := NewOAuthServer(
		secretBytes, &org.LoginRouter{Pds: pds}, dummyDir, db, noop.Meter{}, testStore(t),
		"https://habitat.example", NewJWTBearerStore(), testOpensocialStore(t),
	)
	require.NoError(t, err)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/register":
			oauthSrv.HandleRegister(w, r)
		case "/authorize":
			oauthSrv.HandleAuthorize(w, r)
		case "/oauth-callback":
			oauthSrv.HandleCallback(w, r)
		case "/token":
			oauthSrv.HandleToken(w, r)
		case "/resource":
			credInfo, ok := oauthSrv.Validate(w, r)
			require.True(t, ok, "failed to validate token")
			require.Equal(t, syntax.DID("did:web:example.did.com"), credInfo.Subject)
		default:
			t.Errorf("unknown server path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	server.Client().Jar = jar
	pds.RedirectURI = server.URL + "/oauth-callback"

	// Register the client first, as any MCP client would against
	// /oauth/register before ever hitting /authorize.
	registerReq, err := http.NewRequest(
		http.MethodPost, server.URL+"/register",
		bytes2Reader(t, map[string]any{
			"redirect_uris": []string{server.URL + "/mcp-callback"},
			"client_name":   "Test MCP Client",
		}),
	)
	require.NoError(t, err)
	registerResp, err := server.Client().Do(registerReq)
	require.NoError(t, err)
	defer func() { _ = registerResp.Body.Close() }()
	require.Equal(t, http.StatusOK, registerResp.StatusCode)
	var registered registerResponse
	require.NoError(t, json.NewDecoder(registerResp.Body).Decode(&registered))
	require.NotEmpty(t, registered.ClientID)

	verifier := oauth2.GenerateVerifier()
	config := &oauth2.Config{
		ClientID: registered.ClientID,
		Endpoint: oauth2.Endpoint{
			AuthURL:  server.URL + "/authorize",
			TokenURL: server.URL + "/token",
		},
		RedirectURL: server.URL + "/mcp-callback",
	}

	authRequest, err := http.NewRequest(http.MethodGet, config.AuthCodeURL(
		"test-state",
		oauth2.S256ChallengeOption(verifier),
	)+"&handle=did:web:example.did.com", http.NoBody)
	require.NoError(t, err)

	// The MCP client's redirect target isn't served by this test (it isn't
	// exercising the client's own callback handling, only the server's
	// broker), so it should never actually be reached: intercept it instead
	// of following it.
	server.Client().CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Path == "/mcp-callback" {
			return http.ErrUseLastResponse
		}
		return nil
	}

	result, err := server.Client().Do(authRequest)
	require.NoError(t, err)
	defer func() { _ = result.Body.Close() }()
	require.Equal(t, http.StatusSeeOther, result.StatusCode)

	redirectURL, err := result.Location()
	require.NoError(t, err)
	require.Equal(t, "/mcp-callback", redirectURL.Path)
	code := redirectURL.Query().Get("code")
	require.NotEmpty(t, code)

	oauthClientCtx := context.WithValue(context.Background(), oauth2.HTTPClient, server.Client())
	token, err := config.Exchange(oauthClientCtx, code, oauth2.VerifierOption(verifier))
	require.NoError(t, err)
	require.NotEmpty(t, token.AccessToken)

	client := config.Client(oauthClientCtx, token)
	resp, err := client.Get(server.URL + "/resource")
	require.NoError(t, err)
	respBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode, "resource request failed: %s", respBytes)
}

func mustGenerateKey(t *testing.T) string {
	t.Helper()
	key, err := encrypt.GenerateKey()
	require.NoError(t, err)
	return key
}

func bytes2Reader(t *testing.T, v any) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return bytes.NewReader(b)
}

func TestNormalizeLoopbackRedirect(t *testing.T) {
	db := dbtestutil.NewDB(t)
	secretBytes, err := encrypt.ParseKey(mustGenerateKey(t))
	require.NoError(t, err)
	oauthSrv, err := NewOAuthServer(
		secretBytes, &org.LoginRouter{Pds: login_testutil.NewPassthroughProvider(t)},
		pdsclient.NewDummyDirectory("http://pds.url"), db, noop.Meter{}, testStore(t),
		"https://habitat.example", NewJWTBearerStore(), testOpensocialStore(t),
	)
	require.NoError(t, err)

	// Mirrors Claude Code's client metadata: portless localhost + 127.0.0.1.
	require.NoError(t, oauthSrv.storage.CreateDynamicClient(t.Context(), &oauth.ClientMetadata{
		ClientID:     "native",
		RedirectURIs: []string{"http://localhost/callback", "http://127.0.0.1/callback"},
	}))
	require.NoError(t, oauthSrv.storage.CreateDynamicClient(t.Context(), &oauth.ClientMetadata{
		ClientID:     "exact",
		RedirectURIs: []string{"http://localhost:3000/cb"},
	}))

	form := url.Values{"client_id": {"native"}, "redirect_uri": {"http://localhost:56393/callback"}}
	oauthSrv.normalizeLoopbackRedirect(t.Context(), form)
	require.Equal(t, "http://127.0.0.1:56393/callback", form.Get("redirect_uri"))

	form = url.Values{"client_id": {"exact"}, "redirect_uri": {"http://localhost:3000/cb"}}
	oauthSrv.normalizeLoopbackRedirect(t.Context(), form)
	require.Equal(t, "http://localhost:3000/cb", form.Get("redirect_uri"))
}
