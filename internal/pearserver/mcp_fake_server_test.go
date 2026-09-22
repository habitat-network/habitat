package pearserver_test

import (
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

// newFakeOAuthMcpServer returns an httptest server standing in for an MCP
// server that requires OAuth authorization: it rejects unauthenticated
// requests with a 401 and a WWW-Authenticate challenge, per the MCP
// authorization spec. Discovering and registering with its authorization
// server is Nango's job, not this fake's.
func newFakeOAuthMcpServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(
			"WWW-Authenticate",
			`Bearer resource_metadata="https://example.com/.well-known/oauth-protected-resource"`,
		)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	return srv
}
