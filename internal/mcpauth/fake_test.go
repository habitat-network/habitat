package mcpauth

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// fakeAuthServer is an MCP server and the authorization server protecting it,
// on one httptest server: the server's endpoint is /mcp and the
// authorization server's issuer is the httptest server's root.
type fakeAuthServer struct {
	srv *httptest.Server

	// Settings, fixed before the first request.
	noDCR            bool
	cimdSupported    bool
	noPKCE           bool
	noResourceMeta   bool
	issSupported     bool
	rotateRefresh    bool
	expiresIn        int64
	refreshGate      chan struct{} // if set, refresh grants wait on it
	refreshStarted   chan struct{} // if set, signalled when a refresh grant arrives
	expectedClientID string

	mu            sync.Mutex
	codes         map[string]codeGrant
	refreshTokens map[string]bool
	registrations int
	refreshes     int
	issued        int
}

type codeGrant struct {
	challenge   string
	redirectURI string
	resource    string
}

const (
	fakeClientID     = "client-1"
	fakeClientSecret = "secret-1"
)

func newFakeAuthServer(t *testing.T, configure func(*fakeAuthServer)) *fakeAuthServer {
	t.Helper()
	f := &fakeAuthServer{
		codes:            map[string]codeGrant{},
		refreshTokens:    map[string]bool{},
		expiresIn:        3600,
		expectedClientID: fakeClientID,
	}
	if configure != nil {
		configure(f)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mcp", f.handleMCP)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", f.handleResourceMetadata)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", f.handleAuthServerMetadata)
	mux.HandleFunc("POST /register", f.handleRegister)
	mux.HandleFunc("POST /token", f.handleToken)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAuthServer) issuer() string    { return f.srv.URL }
func (f *fakeAuthServer) serverURL() string { return f.srv.URL + "/mcp" }

// httpClient talks to the fake. It has its own dialer, which opts it out of
// the go-sdk's refusal to fetch metadata from non-public addresses.
func (f *fakeAuthServer) httpClient() *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: (&net.Dialer{}).DialContext}}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeAuthServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	challenge := `Bearer scope="read write"`
	if !f.noResourceMeta {
		challenge += fmt.Sprintf(
			`, resource_metadata="%s/.well-known/oauth-protected-resource/mcp"`,
			f.srv.URL,
		)
	}
	w.Header().Set("WWW-Authenticate", challenge)
	w.WriteHeader(http.StatusUnauthorized)
}

func (f *fakeAuthServer) handleResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if f.noResourceMeta {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":              f.serverURL(),
		"authorization_servers": []string{f.issuer()},
	})
}

func (f *fakeAuthServer) handleAuthServerMetadata(w http.ResponseWriter, r *http.Request) {
	meta := map[string]any{
		"issuer":                                f.issuer(),
		"authorization_endpoint":                f.srv.URL + "/authorize",
		"token_endpoint":                        f.srv.URL + "/token",
		"response_types_supported":              []string{"code"},
		"scopes_supported":                      []string{"read", "write", "offline_access"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic"},
		"code_challenge_methods_supported":      []string{"S256"},
	}
	if f.noPKCE {
		meta["code_challenge_methods_supported"] = []string{"plain"}
	}
	if !f.noDCR {
		meta["registration_endpoint"] = f.srv.URL + "/register"
	}
	if f.cimdSupported {
		meta["client_id_metadata_document_supported"] = true
	}
	if f.issSupported {
		meta["authorization_response_iss_parameter_supported"] = true
	}
	writeJSON(w, http.StatusOK, meta)
}

func (f *fakeAuthServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_client_metadata"})
		return
	}
	f.mu.Lock()
	f.registrations++
	f.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  fakeClientID,
		"client_secret":              fakeClientSecret,
		"token_endpoint_auth_method": "client_secret_basic",
		"redirect_uris":              req.RedirectURIs,
	})
}

// approve plays the member approving the request at authURL, returning the
// code the authorization server would redirect back with.
func (f *fakeAuthServer) approve(t *testing.T, authURL string) (state, code string) {
	t.Helper()
	u, err := url.Parse(authURL)
	require.NoError(t, err)
	require.Equal(t, f.srv.URL+"/authorize", u.Scheme+"://"+u.Host+u.Path)
	q := u.Query()
	require.Equal(t, "code", q.Get("response_type"))
	require.Equal(t, f.expectedClientID, q.Get("client_id"))
	require.Equal(t, "S256", q.Get("code_challenge_method"))
	require.Equal(t, f.serverURL(), q.Get("resource"))

	f.mu.Lock()
	defer f.mu.Unlock()
	f.issued++
	code = fmt.Sprintf("code-%d", f.issued)
	f.codes[code] = codeGrant{
		challenge:   q.Get("code_challenge"),
		redirectURI: q.Get("redirect_uri"),
		resource:    q.Get("resource"),
	}
	return q.Get("state"), code
}

// revokeRefreshTokens makes every refresh token issued so far invalid.
func (f *fakeAuthServer) revokeRefreshTokens() {
	f.mu.Lock()
	defer f.mu.Unlock()
	clear(f.refreshTokens)
}

func (f *fakeAuthServer) refreshCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshes
}

func (f *fakeAuthServer) registrationCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.registrations
}

func (f *fakeAuthServer) handleToken(w http.ResponseWriter, r *http.Request) {
	if id, secret, ok := r.BasicAuth(); !ok || id != fakeClientID || secret != fakeClientSecret {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	if r.PostForm.Get("resource") != f.serverURL() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_target"})
		return
	}

	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		f.mu.Lock()
		grant, ok := f.codes[r.PostForm.Get("code")]
		delete(f.codes, r.PostForm.Get("code"))
		f.mu.Unlock()
		if !ok ||
			grant.redirectURI != r.PostForm.Get("redirect_uri") ||
			grant.challenge != oauth2.S256ChallengeFromVerifier(r.PostForm.Get("code_verifier")) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		writeJSON(w, http.StatusOK, f.issueLocked())

	case "refresh_token":
		if f.refreshStarted != nil {
			f.refreshStarted <- struct{}{}
		}
		if f.refreshGate != nil {
			<-f.refreshGate
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.refreshes++
		old := r.PostForm.Get("refresh_token")
		if !f.refreshTokens[old] {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid_grant", "error_description": "refresh token revoked",
			})
			return
		}
		resp := f.issueLocked()
		if f.rotateRefresh {
			delete(f.refreshTokens, old)
		} else {
			delete(f.refreshTokens, resp["refresh_token"].(string))
			delete(resp, "refresh_token")
		}
		writeJSON(w, http.StatusOK, resp)

	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
	}
}

// issueLocked mints a numbered access and refresh token pair. f.mu is held.
func (f *fakeAuthServer) issueLocked() map[string]any {
	f.issued++
	refresh := fmt.Sprintf("refresh-%d", f.issued)
	f.refreshTokens[refresh] = true
	return map[string]any{
		"access_token":  fmt.Sprintf("access-%d", f.issued),
		"token_type":    "Bearer",
		"expires_in":    f.expiresIn,
		"refresh_token": refresh,
		"scope":         "read write offline_access",
	}
}
