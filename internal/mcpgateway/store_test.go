package mcpgateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/stretchr/testify/require"
)

const (
	testRedirectURL = "https://habitat.example/mcp-oauth-callback"
	testReturnURL   = "https://frontend.example/opensocial/did:plc:org/mcp"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	s, err := NewStore(testutil.NewDB(t), encrypt.TestKey, http.DefaultClient, testRedirectURL)
	require.NoError(t, err)
	return s
}

// fakeOAuthMCPServer is an httptest server standing in for an MCP server
// that requires OAuth authorization: it implements protected resource
// metadata (RFC 9728), authorization server metadata (RFC 8414), dynamic
// client registration (RFC 7591), and a token endpoint, all under one
// origin so it can also serve as its own issuer.
type fakeOAuthMCPServer struct {
	*httptest.Server
	registeredClientID     string
	registeredClientSecret string
	lastCodeVerifier       string
	tokenResponse          func() map[string]any
}

func newFakeOAuthMCPServer(t *testing.T) *fakeOAuthMCPServer {
	t.Helper()
	f := &fakeOAuthMCPServer{
		registeredClientID:     "client-abc",
		registeredClientSecret: "",
	}
	f.tokenResponse = func() map[string]any {
		return map[string]any{
			"access_token":  "access-token-1",
			"refresh_token": "refresh-token-1",
			"token_type":    "Bearer",
			"expires_in":    3600,
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(
			"WWW-Authenticate",
			fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource"`, f.URL),
		)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              f.URL,
			"authorization_servers": []string{f.URL},
		})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                f.URL,
			"authorization_endpoint":                f.URL + "/authorize",
			"token_endpoint":                        f.URL + "/token",
			"registration_endpoint":                 f.URL + "/register",
			"jwks_uri":                              f.URL + "/jwks",
			"response_types_supported":              []string{"code"},
			"code_challenge_methods_supported":      []string{"S256"},
			"token_endpoint_auth_methods_supported": []string{"none"},
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"client_id":     f.registeredClientID,
			"client_secret": f.registeredClientSecret,
			"redirect_uris": []string{testRedirectURL},
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		f.lastCodeVerifier = r.Form.Get("code_verifier")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(f.tokenResponse())
	})

	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func newFakeOpenMCPServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStoreAddServer_DetectsNoAuth(t *testing.T) {
	s := newTestStore(t)
	srv := newFakeOpenMCPServer(t)

	server, err := s.AddServer(t.Context(), syntax.DID("did:plc:org"), "Open", srv.URL, "")
	require.NoError(t, err)
	require.Equal(t, AuthTypeNone, server.AuthType)
}

func TestStoreAddServer_DetectsOAuthAndRegisters(t *testing.T) {
	s := newTestStore(t)
	fake := newFakeOAuthMCPServer(t)

	server, err := s.AddServer(t.Context(), syntax.DID("did:plc:org"), "Linear", fake.URL, "")
	require.NoError(t, err)
	require.Equal(t, AuthTypeOAuth, server.AuthType)
}

func TestStoreAddServer_ListsForOrgOnly(t *testing.T) {
	s := newTestStore(t)
	orgA := syntax.DID("did:plc:org-a")
	orgB := syntax.DID("did:plc:org-b")
	srv := newFakeOpenMCPServer(t)

	server, err := s.AddServer(t.Context(), orgA, "Open", srv.URL, "")
	require.NoError(t, err)
	require.NotEmpty(t, server.ID)
	require.Equal(t, "Open", server.Name)
	require.Equal(t, orgA, server.OrgID)

	otherSrv := newFakeOpenMCPServer(t)
	_, err = s.AddServer(t.Context(), orgB, "Other", otherSrv.URL, "")
	require.NoError(t, err)

	servers, err := s.ListServers(t.Context(), orgA)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.Equal(t, server.ID, servers[0].ID)
}

func TestStoreUpdateServer(t *testing.T) {
	s := newTestStore(t)
	org := syntax.DID("did:plc:org")
	srv := newFakeOpenMCPServer(t)

	server, err := s.AddServer(t.Context(), org, "Open", srv.URL, "desc")
	require.NoError(t, err)

	newName := "Open MCP"
	updated, err := s.UpdateServer(t.Context(), org, server.ID, &newName, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "Open MCP", updated.Name)
	require.Equal(t, srv.URL, updated.URL) // unchanged

	// Wrong org can't update it.
	_, err = s.UpdateServer(t.Context(), syntax.DID("did:plc:other"), server.ID, &newName, nil, nil)
	require.ErrorIs(t, err, ErrServerNotFound)
}

func TestStoreUpdateServer_URLChangeRedetectsAuth(t *testing.T) {
	s := newTestStore(t)
	org := syntax.DID("did:plc:org")
	openSrv := newFakeOpenMCPServer(t)

	server, err := s.AddServer(t.Context(), org, "Server", openSrv.URL, "")
	require.NoError(t, err)
	require.Equal(t, AuthTypeNone, server.AuthType)

	oauthSrv := newFakeOAuthMCPServer(t)
	newURL := oauthSrv.URL
	updated, err := s.UpdateServer(t.Context(), org, server.ID, nil, &newURL, nil)
	require.NoError(t, err)
	require.Equal(t, AuthTypeOAuth, updated.AuthType)
}

func TestStoreRemoveServer_CascadesCredentials(t *testing.T) {
	s := newTestStore(t)
	org := syntax.DID("did:plc:org")
	did := syntax.DID("did:plc:user")
	fake := newFakeOAuthMCPServer(t)

	server, err := s.AddServer(t.Context(), org, "Linear", fake.URL, "")
	require.NoError(t, err)

	completeAuthorization(t, s, did, org, server.ID)
	connected, err := s.IsConnected(t.Context(), did, server.ID)
	require.NoError(t, err)
	require.True(t, connected)

	require.NoError(t, s.RemoveServer(t.Context(), org, server.ID))

	_, err = s.GetServer(t.Context(), org, server.ID)
	require.ErrorIs(t, err, ErrServerNotFound)

	_, err = s.GetAccessToken(t.Context(), did, server.ID)
	require.ErrorIs(t, err, ErrCredentialNotFound)
}

func TestStoreRemoveServer_NotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.RemoveServer(t.Context(), syntax.DID("did:plc:org"), ServerID("nonexistent"))
	require.ErrorIs(t, err, ErrServerNotFound)
}

func TestStoreStartAuthorization_NotOAuthServer(t *testing.T) {
	s := newTestStore(t)
	org := syntax.DID("did:plc:org")
	srv := newFakeOpenMCPServer(t)

	server, err := s.AddServer(t.Context(), org, "Open", srv.URL, "")
	require.NoError(t, err)

	_, err = s.StartAuthorization(t.Context(), syntax.DID("did:plc:user"), org, server.ID, testReturnURL)
	require.ErrorIs(t, err, ErrNotOAuthServer)
}

func TestStoreAuthorizationFlow(t *testing.T) {
	s := newTestStore(t)
	org := syntax.DID("did:plc:org")
	did := syntax.DID("did:plc:user")
	fake := newFakeOAuthMCPServer(t)

	server, err := s.AddServer(t.Context(), org, "Linear", fake.URL, "")
	require.NoError(t, err)

	connected, err := s.IsConnected(t.Context(), did, server.ID)
	require.NoError(t, err)
	require.False(t, connected)

	authURL, err := s.StartAuthorization(t.Context(), did, org, server.ID, testReturnURL)
	require.NoError(t, err)
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	require.Equal(t, fake.URL+"/authorize", parsed.Scheme+"://"+parsed.Host+parsed.Path)
	state := parsed.Query().Get("state")
	require.NotEmpty(t, state)
	require.Equal(t, "S256", parsed.Query().Get("code_challenge_method"))
	require.NotEmpty(t, parsed.Query().Get("code_challenge"))
	require.Equal(t, fake.URL, parsed.Query().Get("resource"))

	gotDID, gotOrg, gotID, gotReturnURL, err := s.CompleteAuthorization(t.Context(), state, "fake-code")
	require.NoError(t, err)
	require.Equal(t, did, gotDID)
	require.Equal(t, org, gotOrg)
	require.Equal(t, server.ID, gotID)
	require.Equal(t, testReturnURL, gotReturnURL)
	require.NotEmpty(t, fake.lastCodeVerifier)

	connected, err = s.IsConnected(t.Context(), did, server.ID)
	require.NoError(t, err)
	require.True(t, connected)

	token, err := s.GetAccessToken(t.Context(), did, server.ID)
	require.NoError(t, err)
	require.Equal(t, "access-token-1", token)

	// The state is single-use.
	_, _, _, _, err = s.CompleteAuthorization(t.Context(), state, "fake-code")
	require.ErrorIs(t, err, ErrPendingAuthorizationNotFound)
}

func TestStoreCompleteAuthorization_UnknownState(t *testing.T) {
	s := newTestStore(t)
	_, _, _, _, err := s.CompleteAuthorization(t.Context(), "bogus-state", "code")
	require.ErrorIs(t, err, ErrPendingAuthorizationNotFound)
}

func TestStoreStartAuthorization_RejectsInsecureReturnURL(t *testing.T) {
	s := newTestStore(t)
	org := syntax.DID("did:plc:org")
	fake := newFakeOAuthMCPServer(t)

	server, err := s.AddServer(t.Context(), org, "Linear", fake.URL, "")
	require.NoError(t, err)

	_, err = s.StartAuthorization(
		t.Context(), syntax.DID("did:plc:user"), org, server.ID, "http://evil.example/steal",
	)
	require.Error(t, err)
}

func TestStoreGetAccessToken_RefreshesExpiredToken(t *testing.T) {
	s := newTestStore(t)
	org := syntax.DID("did:plc:org")
	did := syntax.DID("did:plc:user")
	fake := newFakeOAuthMCPServer(t)
	fake.tokenResponse = func() map[string]any {
		return map[string]any{
			"access_token":  "access-token-1",
			"refresh_token": "refresh-token-1",
			"token_type":    "Bearer",
			"expires_in":    -3600, // already expired
		}
	}

	server, err := s.AddServer(t.Context(), org, "Linear", fake.URL, "")
	require.NoError(t, err)
	completeAuthorization(t, s, did, org, server.ID)

	fake.tokenResponse = func() map[string]any {
		return map[string]any{
			"access_token": "access-token-2",
			"token_type":   "Bearer",
			"expires_in":   3600,
		}
	}

	token, err := s.GetAccessToken(t.Context(), did, server.ID)
	require.NoError(t, err)
	require.Equal(t, "access-token-2", token)
}

func TestStoreDisconnectServer(t *testing.T) {
	s := newTestStore(t)
	org := syntax.DID("did:plc:org")
	did := syntax.DID("did:plc:user")
	fake := newFakeOAuthMCPServer(t)

	server, err := s.AddServer(t.Context(), org, "Linear", fake.URL, "")
	require.NoError(t, err)
	completeAuthorization(t, s, did, org, server.ID)

	require.NoError(t, s.DisconnectServer(t.Context(), did, server.ID))
	connected, err := s.IsConnected(t.Context(), did, server.ID)
	require.NoError(t, err)
	require.False(t, connected)

	err = s.DisconnectServer(t.Context(), did, server.ID)
	require.ErrorIs(t, err, ErrCredentialNotFound)
}

func TestStoreGetAccessToken_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetAccessToken(t.Context(), syntax.DID("did:plc:user"), ServerID("none"))
	require.ErrorIs(t, err, ErrCredentialNotFound)
}

func TestProtectedResourceMetadataURLs(t *testing.T) {
	got := protectedResourceMetadataURLs("", "https://mcp.example.com/sse")
	require.Equal(t, []prmCandidate{
		{url: "https://mcp.example.com/.well-known/oauth-protected-resource/sse", resource: "https://mcp.example.com/sse"},
		{url: "https://mcp.example.com/.well-known/oauth-protected-resource", resource: "https://mcp.example.com"},
	}, got)
}

func TestAuthServerMetadataURL(t *testing.T) {
	u, err := authServerMetadataURL("https://auth.example.com")
	require.NoError(t, err)
	require.Equal(t, "https://auth.example.com/.well-known/oauth-authorization-server", u)

	u, err = authServerMetadataURL("https://auth.example.com/tenant1")
	require.NoError(t, err)
	require.Equal(t, "https://auth.example.com/.well-known/oauth-authorization-server/tenant1", u)
}

func completeAuthorization(
	t *testing.T,
	s Store,
	did, org syntax.DID,
	id ServerID,
) {
	t.Helper()
	authURL, err := s.StartAuthorization(t.Context(), did, org, id, testReturnURL)
	require.NoError(t, err)
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	require.NotEmpty(t, state)
	_, _, _, _, err = s.CompleteAuthorization(t.Context(), state, "fake-code")
	require.NoError(t, err)
}
