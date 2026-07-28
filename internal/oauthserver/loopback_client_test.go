package oauthserver

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsLoopbackClientID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"bare origin", "http://localhost", true},
		{"trailing slash", "http://localhost/", true},
		{"with query", "http://localhost?scope=atproto", true},
		{"https rejected", "https://localhost", false},
		{"loopback IP rejected", "http://127.0.0.1", false},
		{"unrelated https URL", "https://example.com/client-metadata.json", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isLoopbackClientID(tt.id))
		})
	}
}

func TestParseLoopbackClientID(t *testing.T) {
	tests := []struct {
		name             string
		id               string
		wantScope        string
		wantRedirectURIs []string
		wantErr          string
	}{
		{
			name:             "bare origin uses defaults",
			id:               "http://localhost",
			wantScope:        "atproto",
			wantRedirectURIs: []string{"http://127.0.0.1/", "http://[::1]/"},
		},
		{
			name:             "single trailing slash uses defaults",
			id:               "http://localhost/",
			wantScope:        "atproto",
			wantRedirectURIs: []string{"http://127.0.0.1/", "http://[::1]/"},
		},
		{
			name:             "explicit atproto scope",
			id:               "http://localhost?scope=atproto",
			wantScope:        "atproto",
			wantRedirectURIs: []string{"http://127.0.0.1/", "http://[::1]/"},
		},
		{
			name:             "scope with additional values",
			id:               "http://localhost?scope=atproto+transition:generic",
			wantScope:        "atproto transition:generic",
			wantRedirectURIs: []string{"http://127.0.0.1/", "http://[::1]/"},
		},
		{
			name:    "scope missing atproto is rejected",
			id:      "http://localhost?scope=transition:generic",
			wantErr: `must include "atproto"`,
		},
		{
			name:             "custom redirect_uri",
			id:               "http://localhost?redirect_uri=http://127.0.0.1:8080/cb",
			wantScope:        "atproto",
			wantRedirectURIs: []string{"http://127.0.0.1:8080/cb"},
		},
		{
			name:      "repeated redirect_uri",
			id:        "http://localhost?redirect_uri=http://127.0.0.1:8080/cb&redirect_uri=http://[::1]:9090/cb",
			wantScope: "atproto",
			wantRedirectURIs: []string{
				"http://127.0.0.1:8080/cb",
				"http://[::1]:9090/cb",
			},
		},
		{
			name:    "redirect_uri using localhost is rejected",
			id:      "http://localhost?redirect_uri=http://localhost:8080/cb",
			wantErr: `"localhost" hostname is not allowed`,
		},
		{
			name:    "redirect_uri using https is rejected",
			id:      "http://localhost?redirect_uri=https://127.0.0.1/cb",
			wantErr: `must use the "http:" protocol`,
		},
		{
			name:    "real path component is rejected",
			id:      "http://localhost/oauth/callback",
			wantErr: "path component",
		},
		{
			name:    "fragment is rejected",
			id:      "http://localhost#frag",
			wantErr: "hash component",
		},
		{
			name:    "unexpected query key is rejected",
			id:      "http://localhost?foo=bar",
			wantErr: `unexpected query parameter "foo"`,
		},
		{
			name:    "duplicate scope key is rejected",
			id:      "http://localhost?scope=atproto&scope=atproto",
			wantErr: `duplicate "scope"`,
		},
		{
			name:    "not a loopback client id at all",
			id:      "https://example.com",
			wantErr: `must start with "http://localhost"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope, redirectURIs, err := parseLoopbackClientID(tt.id)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantScope, scope)
			require.Equal(t, tt.wantRedirectURIs, redirectURIs)
		})
	}
}

func TestSynthesizeLoopbackClientMetadata(t *testing.T) {
	metadata, err := synthesizeLoopbackClientMetadata(
		"http://localhost?redirect_uri=http://127.0.0.1:8080/cb",
	)
	require.NoError(t, err)
	require.Equal(t, "http://localhost?redirect_uri=http://127.0.0.1:8080/cb", metadata.ClientId)
	require.Equal(t, "atproto", metadata.Scope)
	require.Equal(t, []string{"http://127.0.0.1:8080/cb"}, metadata.RedirectUris)
	require.Equal(t, []string{"code"}, metadata.ResponseTypes)
	require.Equal(t, []string{"authorization_code", "refresh_token"}, metadata.GrantTypes)
	require.Equal(t, "none", metadata.TokenEndpointAuthMethod)
	require.Equal(t, "native", metadata.ApplicationType)
	require.True(t, metadata.DpopBoundAccessTokens)
}

func TestSynthesizeLoopbackClientMetadata_InvalidID(t *testing.T) {
	_, err := synthesizeLoopbackClientMetadata("http://localhost/some/path")
	require.Error(t, err)
}
