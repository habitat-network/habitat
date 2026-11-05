package mcpgateway

import (
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
