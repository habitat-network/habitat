package mcpgateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestTokenAuthMethod(t *testing.T) {
	for _, tc := range []struct {
		methods []string
		want    string
		style   oauth2.AuthStyle
	}{
		{nil, "client_secret_basic", oauth2.AuthStyleInHeader},
		{[]string{"none"}, "none", oauth2.AuthStyleInParams},
		{[]string{"client_secret_basic", "none"}, "client_secret_basic", oauth2.AuthStyleInHeader},
		{[]string{"client_secret_basic", "client_secret_post"}, "client_secret_post", oauth2.AuthStyleInParams},
		{[]string{"private_key_jwt"}, "none", oauth2.AuthStyleInParams},
	} {
		require.Equal(t, tc.want, tokenAuthMethod(tc.methods), tc.methods)
		require.Equal(t, tc.style, tokenAuthStyle(tc.methods), tc.methods)
	}
	require.Equal(t, oauth2.AuthStyleAutoDetect, authMethodStyle("private_key_jwt"))
}

func TestProtectedResourceMetadataURLs(t *testing.T) {
	challenges := []oauthex.Challenge{{
		Scheme: "bearer",
		Params: map[string]string{"resource_metadata": "https://as.example.com/prm"},
	}}
	require.Equal(t, []prmCandidate{
		{url: "https://as.example.com/prm", resource: "https://mcp.example.com/v1/mcp"},
		{
			url:      "https://mcp.example.com/.well-known/oauth-protected-resource/v1/mcp",
			resource: "https://mcp.example.com/v1/mcp",
		},
		{
			url:      "https://mcp.example.com/.well-known/oauth-protected-resource",
			resource: "https://mcp.example.com",
		},
	}, protectedResourceMetadataURLs(challenges, "https://mcp.example.com/v1/mcp"))
}

func TestProtectedResourceMetadataFallsBackToServerRoot(t *testing.T) {
	// Nothing answers at 127.0.0.1:1, so no metadata is found and the server
	// is taken to be its own authorization server (MCP 2025-03-26).
	prm := protectedResourceMetadata(t.Context(), oauth2.NewClient(t.Context(), nil), nil,
		"http://127.0.0.1:1/mcp?x=1")
	require.Equal(t, []string{"http://127.0.0.1:1"}, prm.AuthorizationServers)
	require.Equal(t, "http://127.0.0.1:1/mcp?x=1", prm.Resource)
}

func TestDiscoverOAuthRejectsInvalidURL(t *testing.T) {
	_, err := discoverOAuth(
		t.Context(),
		nil,
		"file:///etc/passwd",
		"https://pear.example.com/cb",
		nil,
	)
	require.ErrorIs(t, err, ErrInvalidServerURL)
}

func TestServerURLPattern(t *testing.T) {
	for u, want := range map[string]bool{
		"https://mcp.example.com":         true,
		"https://mcp.example.com/mcp?x=1": true,
		"http://localhost:8080/mcp":       true,
		"https://u:p@mcp.example.com/mcp": false,
		"ftp://mcp.example.com":           false,
		"https:///mcp":                    false,
		"https://mcp.example.com/a b":     false,
	} {
		require.Equal(t, want, serverURLPattern.MatchString(u), u)
	}
}

func TestAuthorizationURLRequestsRefreshToken(t *testing.T) {
	cfg := &oauthConfig{
		Resource: "https://mcp.example.com",
		AuthURL:  "https://auth.example.com/authorize",
		ClientID: "client",
	}
	u, err := url.Parse(cfg.authorizationURL("https://pear.example.com/cb", "state", "verifier"))
	require.NoError(t, err)
	q := u.Query()
	require.Equal(t, "offline", q.Get("access_type"))
	require.Equal(t, "consent", q.Get("prompt"))
	require.Equal(t, "https://mcp.example.com", q.Get("resource"))
	require.Equal(t, "S256", q.Get("code_challenge_method"))
}

// Grafana Cloud serves /mcp/<stack> but publishes the resource /mcp.
func TestProtectedResourceMetadataAcceptsEnclosingResource(t *testing.T) {
	var srv *httptest.Server
	handler := func(resource string, as []string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resource": resource, "authorization_servers": as,
			})
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/prm/ok", func(w http.ResponseWriter, r *http.Request) {
		handler(srv.URL+"/mcp", []string{srv.URL + "/mcp/stack"})(w, r)
	})
	mux.HandleFunc("/prm/other-host", func(w http.ResponseWriter, r *http.Request) {
		handler("https://evil.example.com/mcp", []string{srv.URL})(w, r)
	})
	mux.HandleFunc("/prm/sibling-path", func(w http.ResponseWriter, r *http.Request) {
		handler(srv.URL+"/mc", []string{srv.URL})(w, r)
	})
	mux.HandleFunc("/prm/http-as", func(w http.ResponseWriter, r *http.Request) {
		handler(srv.URL+"/mcp", []string{"http://as.example.com"})(w, r)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	challenge := func(path string) []oauthex.Challenge {
		return []oauthex.Challenge{{
			Scheme: "bearer",
			Params: map[string]string{"resource_metadata": srv.URL + path},
		}}
	}
	serverURL := srv.URL + "/mcp/stack"

	prm := protectedResourceMetadata(t.Context(), srv.Client(), challenge("/prm/ok"), serverURL)
	require.Equal(t, []string{srv.URL + "/mcp/stack"}, prm.AuthorizationServers)

	for _, path := range []string{"/prm/other-host", "/prm/sibling-path", "/prm/http-as"} {
		prm := protectedResourceMetadata(t.Context(), srv.Client(), challenge(path), serverURL)
		require.Equal(t, []string{srv.URL}, prm.AuthorizationServers, path)
	}
}
