package oauthserver

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/ory/fosite"
)

// scopeMCP is the only scope MCP clients normally request; it carries no
// meaning of its own to the shared ScopeStrategy.
const scopeMCP = "mcp"

// Paths, relative to the origin, of the MCP-shaped endpoints OAuthServer
// serves. They match the interface MCP clients expect — RFC 7591 dynamic
// client registration instead of client-id metadata documents, no PAR, and
// mandatory PKCE — but share the atproto endpoints' fosite.OAuth2Provider,
// storage, sign-in (org.LoginRouter), and callback (HandleCallback): once the
// client is identified and the user is prompted for their handle
// (HandleMCPAuthorize, HandleMCPAuthorizeSubmit), the rest of the flow is the
// same pipeline atproto clients go through, and PDS/Google logins both
// endpoint sets initiate land back at the one /oauth-callback the login
// providers are registered with. See issuerFor and OAuthServer.ValidateRaw
// for how a single token type serves both.
const (
	MCPMetadataPath  = "/.well-known/oauth-authorization-server/mcp"
	MCPRegisterPath  = "/mcp/oauth/register"
	MCPAuthorizePath = "/mcp/oauth/authorize"
	// MCPAuthorizePagePath is the pear-pages route the browser is redirected to
	// for the handle prompt; it POSTs the handle to MCPAuthorizeSubmitPath.
	MCPAuthorizePagePath   = "/ui/login/mcp"
	MCPAuthorizeSubmitPath = "/mcp/oauth/authorize/submit"
	MCPTokenPath           = "/mcp/oauth/token"
	// MCPIssuerPath and MCPResourcePath are the paths of the MCP issuer and
	// resource, relative to the origin.
	MCPIssuerPath   = "/mcp"
	MCPResourcePath = "/mcp"
)

// MCPIssuer returns the authorization server identifier MCP clients discover
// through RFC 8414 metadata (MCPMetadataPath).
func (o *OAuthServer) MCPIssuer() string { return o.issuer + MCPIssuerPath }

func (o *OAuthServer) mcpResource() string { return o.issuer + MCPResourcePath }

// HandleMCPMetadata serves RFC 8414 authorization server metadata for the MCP
// endpoints.
func (o *OAuthServer) HandleMCPMetadata(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(r.Context(), w, buildMCPAuthServerMetadata(o.issuer))
}

// mcpRegisterRequest is the RFC 7591 registration request. Fields this server
// doesn't act on are ignored.
type mcpRegisterRequest struct {
	RedirectURIs []string `json:"redirect_uris"`
	GrantTypes   []string `json:"grant_types"`
	ClientName   string   `json:"client_name"`
}

// HandleMCPRegister implements RFC 7591 dynamic client registration.
// Registered clients are always public and must use PKCE.
func (o *OAuthServer) HandleMCPRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req mcpRegisterRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeMCPOAuthError(ctx, w, "invalid_client_metadata", "failed to decode request body")
		return
	}
	if len(req.RedirectURIs) == 0 {
		writeMCPOAuthError(ctx, w, "invalid_redirect_uri", "redirect_uris is required")
		return
	}
	for _, raw := range req.RedirectURIs {
		if err := validateRedirectURI(raw); err != nil {
			writeMCPOAuthError(ctx, w, "invalid_redirect_uri", err.Error())
			return
		}
	}
	grantTypes := req.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code", "refresh_token"}
	}
	for _, g := range grantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			writeMCPOAuthError(ctx, w, "invalid_client_metadata", "unsupported grant type "+g)
			return
		}
	}

	clientID, err := randomToken(24)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("generate client id: %w", err))
		return
	}
	clientID = "mcp-" + clientID
	uris, _ := json.Marshal(req.RedirectURIs)
	grants, _ := json.Marshal(grantTypes)
	if err := o.storage.createRegisteredClient(ctx, &RegisteredClient{
		ClientID: clientID, ClientName: req.ClientName, RedirectURIs: uris, GrantTypes: grants,
	}); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("register client: %w", err))
		return
	}
	// Headers are frozen at WriteHeader, so the content type WriteJSON sets
	// would be dropped; set it first.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	httpx.WriteJSON(ctx, w, map[string]any{
		"client_id":                  clientID,
		"client_id_issued_at":        time.Now().Unix(),
		"client_name":                req.ClientName,
		"redirect_uris":              req.RedirectURIs,
		"grant_types":                grantTypes,
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

// validateRedirectURI accepts https URIs, http loopback URIs, and private-use
// custom schemes used by native apps (RFC 8252), and rejects the rest.
func validateRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Fragment != "" {
		return fmt.Errorf("invalid redirect uri %q", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		switch u.Hostname() {
		case "localhost", "127.0.0.1", "::1":
			return nil
		}
		return fmt.Errorf("http redirect uris must be loopback: %q", raw)
	case "javascript", "data", "file", "ftp", "blob", "about":
		return fmt.Errorf("unsupported redirect uri scheme %q", u.Scheme)
	}
	return nil
}

// HandleMCPAuthorize validates the client's authorization request — PKCE and
// scope are enforced by the shared fosite config (see NewOAuthServer), only
// the "resource" parameter (RFC 8707) is MCP-specific — and redirects the
// browser to the pear-pages handle prompt (MCPAuthorizePagePath), storing the
// pending request under the same request-key cookie HandleAuthorize uses so
// the shared HandleCallback can pick it up once the user signs in. That page
// POSTs the entered handle back to HandleMCPAuthorizeSubmit.
func (o *OAuthServer) HandleMCPAuthorize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session, err := o.sessionStore.Get(r, sessionName)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to get cookie: %w", err))
		return
	}
	ar, err := o.provider.NewAuthorizeRequest(ctx, r)
	if err != nil {
		o.provider.WriteAuthorizeError(ctx, w, ar, err)
		return
	}
	if resource := r.Form.Get("resource"); resource != "" && resource != o.mcpResource() {
		o.provider.WriteAuthorizeError(
			ctx,
			w,
			ar,
			fosite.ErrInvalidRequest.WithHint(
				"The 'resource' parameter does not identify this MCP server.",
			),
		)
		return
	}
	// fosite only validates PKCE when generating the authorize *response*
	// (finishAuthorize, after the user signs in), which is too late to tell an
	// MCP client its request was malformed. Check it eagerly here instead.
	if r.Form.Get("code_challenge") == "" || r.Form.Get("code_challenge_method") != "S256" {
		o.provider.WriteAuthorizeError(
			ctx,
			w,
			ar,
			fosite.ErrInvalidRequest.WithHint(
				"PKCE with the S256 code challenge method is required.",
			),
		)
		return
	}
	ar.SetSession(newSession())
	requestKey, err := randomToken(24)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("generate request id: %w", err))
		return
	}
	if err := o.storage.CreatePARSession(ctx, requestKey, ar); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("save request: %w", err))
		return
	}
	session.Values[requestKeyCookie] = requestKey
	if err := session.Save(r, w); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to save cookie: %w", err))
		return
	}
	clientName := ""
	if c, ok := ar.GetClient().(clientDisplay); ok {
		clientName = c.displayName()
	}
	page := url.Values{
		"client_name": {clientName},
		"login_hint":  {r.Form.Get("login_hint")},
	}
	http.Redirect(w, r, MCPAuthorizePagePath+"?"+page.Encode(), http.StatusSeeOther)
}

// mcpAuthorizeSubmitRequest is the JSON body the pear-pages handle prompt
// POSTs to HandleMCPAuthorizeSubmit.
type mcpAuthorizeSubmitRequest struct {
	Handle string `json:"handle"`
}

// HandleMCPAuthorizeSubmit resolves the handle submitted from
// MCPAuthorizePagePath to a DID and starts that DID's sign-in through
// o.loginRouter — the same routing HandleAuthorize uses, so an MCP client
// signs a user in exactly the way the org they belong to requires (PDS,
// Google, or password). The pending request is found via the request-key
// cookie HandleMCPAuthorize set; the provider's opaque flow state is stashed
// back into that same cookie for the shared HandleCallback to pick up once
// the login completes.
func (o *OAuthServer) HandleMCPAuthorizeSubmit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session, err := o.sessionStore.Get(r, sessionName)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to get cookie: %w", err))
		return
	}
	requestKey, _ := session.Values[requestKeyCookie].(string)
	if _, err := o.storage.GetPARSession(ctx, requestKey); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "unknown or expired authorization request", err)
		return
	}

	var req mcpAuthorizeSubmitRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid request body", err)
		return
	}
	handle := strings.TrimPrefix(strings.TrimSpace(req.Handle), "@")
	if handle == "" {
		httpx.WriteError(ctx, w, "InvalidRequest", "Enter your handle.", http.StatusBadRequest)
		return
	}
	subject, err := o.resolveLoginHint(ctx, handle)
	if err != nil || subject == "" {
		httpx.WriteError(
			ctx, w, "InvalidRequest", "Couldn't sign in with that handle. Check it and try again.",
			http.StatusBadRequest,
		)
		return
	}
	if err := o.storage.UpdatePARSessionSubject(ctx, requestKey, subject); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to update request subject: %w", err))
		return
	}

	redirect, providerState, err := o.beginLogin(ctx, subject)
	if err != nil {
		httpx.WriteError(
			ctx, w, "InvalidRequest", "Couldn't sign in with that handle. Check it and try again.",
			http.StatusBadRequest,
		)
		return
	}
	session.Values[providerStateCookie] = providerState
	if err := session.Save(r, w); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to save cookie: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, map[string]string{"redirect": redirect})
}

func writeMCPOAuthError(ctx context.Context, w http.ResponseWriter, code, description string) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusBadRequest)
	httpx.WriteJSON(ctx, w, map[string]string{"error": code, "error_description": description})
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
