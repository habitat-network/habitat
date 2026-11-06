// Package mcpgatewaytest provides a fake OAuth-protected MCP server for tests
// of internal/mcpgateway and its callers: an MCP endpoint, its protected
// resource metadata, and an authorization server that approves every request.
package mcpgatewaytest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// ToolName is the one tool the fake MCP server offers. It echoes its "text"
// argument back.
const ToolName = "echo"

// Server is a fake OAuth-protected MCP server.
type Server struct {
	// URL is the MCP endpoint.
	URL string
	// Issuer is the authorization server's issuer.
	Issuer string

	base string

	mu sync.Mutex
	// dcr is whether the authorization server supports dynamic client
	// registration.
	dcr bool
	// accessTTL is how long issued access tokens live.
	accessTTL time.Duration
	// refreshedTTL is how long access tokens issued by a refresh live; zero
	// issues them without an expiry. Negative is accessTTL.
	refreshedTTL time.Duration
	clients      map[string]string // client id -> redirect uri
	codes        map[string]codeGrant
	access       map[string]time.Time // access token -> expiry
	refresh      map[string]bool      // live refresh tokens
	// Refreshes counts refresh grants redeemed.
	Refreshes int
	// failTokens makes the token endpoint answer with a server error.
	failTokens bool
}

type codeGrant struct {
	clientID  string
	challenge string
	resource  string
}

// Option configures a Server.
type Option func(*Server)

// WithoutDCR makes the authorization server refuse dynamic client
// registration, so clients must be pre-registered with RegisterClient.
func WithoutDCR() Option { return func(s *Server) { s.dcr = false } }

// WithAccessTokenTTL sets how long issued access tokens live.
func WithAccessTokenTTL(ttl time.Duration) Option {
	return func(s *Server) { s.accessTTL = ttl }
}

// WithRefreshedTokenTTL sets how long access tokens issued by a refresh
// live, instead of the WithAccessTokenTTL. Zero issues them without
// expires_in, as many authorization servers do.
func WithRefreshedTokenTTL(ttl time.Duration) Option {
	return func(s *Server) { s.refreshedTTL = ttl }
}

// New starts a fake OAuth-protected MCP server, stopped when t ends.
func New(t *testing.T, opts ...Option) *Server {
	t.Helper()
	s := &Server{
		dcr:          true,
		accessTTL:    time.Hour,
		refreshedTTL: -1,
		clients:      make(map[string]string),
		codes:        make(map[string]codeGrant),
		access:       make(map[string]time.Time),
		refresh:      make(map[string]bool),
	}
	for _, opt := range opts {
		opt(s)
	}

	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "0.0.1"}, nil)
	type echoInput struct {
		Text string `json:"text"`
	}
	mcp.AddTool(
		mcpServer,
		&mcp.Tool{Name: ToolName, Description: "Echoes text."},
		func(_ context.Context, _ *mcp.CallToolRequest, in echoInput) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: in.Text}},
			}, nil, nil
		},
	)
	streamable := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return mcpServer }, nil,
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || !s.validAccess(token) {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+
				s.base+`/.well-known/oauth-protected-resource/mcp", scope="read"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		streamable.ServeHTTP(w, r)
	})
	mux.HandleFunc(
		"/.well-known/oauth-protected-resource/mcp",
		func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"resource":              s.URL,
				"authorization_servers": []string{s.Issuer},
				"scopes_supported":      []string{"read"},
			})
		},
	)
	mux.HandleFunc(
		"/.well-known/oauth-authorization-server",
		func(w http.ResponseWriter, _ *http.Request) {
			meta := map[string]any{
				"issuer":                           s.Issuer,
				"authorization_endpoint":           s.base + "/authorize",
				"token_endpoint":                   s.base + "/token",
				"response_types_supported":         []string{"code"},
				"code_challenge_methods_supported": []string{"S256"},
				"scopes_supported": []string{
					"read",
					"offline_access",
				},
				"token_endpoint_auth_methods_supported":          []string{"none"},
				"authorization_response_iss_parameter_supported": true,
			}
			s.mu.Lock()
			if s.dcr {
				meta["registration_endpoint"] = s.base + "/register"
			}
			s.mu.Unlock()
			writeJSON(w, http.StatusOK, meta)
		},
	)
	mux.HandleFunc("/register", s.handleRegister)
	mux.HandleFunc("/authorize", s.handleAuthorize)
	mux.HandleFunc("/token", s.handleToken)

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	s.base = ts.URL
	s.URL = ts.URL + "/mcp"
	s.Issuer = ts.URL
	return s
}

// RegisterClient pre-registers a client redirecting to redirectURI, for a
// server made WithoutDCR, and returns its ID.
func (s *Server) RegisterClient(redirectURI string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := "client-" + rand.Text()
	s.clients[id] = redirectURI
	return id
}

// Authorize plays the member's browser: it follows authorizationURL, which the
// fake approves, and returns the query of the redirect back to the client
// (code, state and iss).
func (s *Server) Authorize(t *testing.T, authorizationURL string) url.Values {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(authorizationURL)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusFound, resp.StatusCode)
	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	return loc.Query()
}

// ExpireAccessTokens makes every access token issued so far invalid, as if
// they'd expired.
func (s *Server) ExpireAccessTokens() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.access)
}

// RevokeRefreshTokens makes every refresh token issued so far invalid.
func (s *Server) RevokeRefreshTokens() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.refresh)
}

func (s *Server) validAccess(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	expiry, ok := s.access[token]
	return ok && time.Now().Before(expiry)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.RedirectURIs) != 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_client_metadata"})
		return
	}
	id := s.RegisterClient(req.RedirectURIs[0])
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  id,
		"redirect_uris":              req.RedirectURIs,
		"token_endpoint_auth_method": "none",
	})
}

func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.mu.Lock()
	redirect, ok := s.clients[q.Get("client_id")]
	s.mu.Unlock()
	if !ok || redirect != q.Get("redirect_uri") || q.Get("code_challenge_method") != "S256" ||
		q.Get("resource") != s.URL {
		http.Error(w, "bad authorization request", http.StatusBadRequest)
		return
	}
	code := rand.Text()
	s.mu.Lock()
	s.codes[code] = codeGrant{
		clientID:  q.Get("client_id"),
		challenge: q.Get("code_challenge"),
		resource:  q.Get("resource"),
	}
	s.mu.Unlock()
	back, _ := url.Parse(redirect)
	bq := back.Query()
	bq.Set("code", code)
	bq.Set("state", q.Get("state"))
	bq.Set("iss", s.Issuer)
	back.RawQuery = bq.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

// FailTokenRequests makes the token endpoint fail with a server error, as in
// an outage, or stops it failing.
func (s *Server) FailTokenRequests(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failTokens = fail
}

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	fail := s.failTokens
	s.mu.Unlock()
	if fail {
		writeJSON(
			w,
			http.StatusServiceUnavailable,
			map[string]string{"error": "temporarily_unavailable"},
		)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	if r.Form.Get("resource") != s.URL {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_target"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		grant, ok := s.codes[r.Form.Get("code")]
		delete(s.codes, r.Form.Get("code"))
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || grant.clientID != r.Form.Get("client_id") ||
			base64.RawURLEncoding.EncodeToString(sum[:]) != grant.challenge {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
			return
		}
	case "refresh_token":
		if !s.refresh[r.Form.Get("refresh_token")] {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
			return
		}
		// Refresh tokens rotate.
		delete(s.refresh, r.Form.Get("refresh_token"))
		s.Refreshes++
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
		return
	}
	ttl := s.accessTTL
	if r.Form.Get("grant_type") == "refresh_token" && s.refreshedTTL >= 0 {
		ttl = s.refreshedTTL
	}
	access, refresh := "at-"+rand.Text(), "rt-"+rand.Text()
	resp := map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"refresh_token": refresh,
	}
	if ttl > 0 {
		s.access[access] = time.Now().Add(ttl)
		resp["expires_in"] = int(ttl.Seconds())
	} else {
		s.access[access] = time.Now().Add(100 * 365 * 24 * time.Hour)
	}
	s.refresh[refresh] = true
	writeJSON(w, http.StatusOK, resp)
}

// RefreshCount is how many refresh grants have been redeemed.
func (s *Server) RefreshCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Refreshes
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
