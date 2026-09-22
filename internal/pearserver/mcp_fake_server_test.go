package pearserver_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newFakeOpenMcpServer returns an httptest server standing in for an MCP
// server that requires no per-user authorization: any request succeeds.
func newFakeOpenMcpServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fakeOAuthMcpServer stands in for an MCP server that requires OAuth
// authorization, implementing protected resource metadata (RFC 9728),
// authorization server metadata (RFC 8414), dynamic client registration
// (RFC 7591), and a token endpoint under one origin.
type fakeOAuthMcpServer struct {
	*httptest.Server
}

func newFakeOAuthMcpServer(t *testing.T) *fakeOAuthMcpServer {
	t.Helper()
	f := &fakeOAuthMcpServer{}

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
			"issuer":                           f.URL,
			"authorization_endpoint":           f.URL + "/authorize",
			"token_endpoint":                   f.URL + "/token",
			"registration_endpoint":            f.URL + "/register",
			"jwks_uri":                         f.URL + "/jwks",
			"response_types_supported":         []string{"code"},
			"code_challenge_methods_supported": []string{"S256"},
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"client_id": "client-abc",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-token-1",
			"refresh_token": "refresh-token-1",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	})

	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}
