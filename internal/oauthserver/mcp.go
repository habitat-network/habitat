package oauthserver

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
// serves. They mirror the atproto endpoints (HandleAuthorize, HandleToken,
// etc.) but match the interface MCP clients expect: RFC 7591 dynamic client
// registration instead of client-id metadata documents, no PAR, and PKCE is
// mandatory. Both sets of endpoints share the same fosite.OAuth2Provider and
// storage, so a token issued by either is valid everywhere: see
// OAuthServer.ValidateRaw.
const (
	MCPMetadataPath  = "/.well-known/oauth-authorization-server/mcp"
	MCPRegisterPath  = "/mcp/oauth/register"
	MCPAuthorizePath = "/mcp/oauth/authorize"
	// MCPAuthorizePagePath is the pear-pages route the browser is redirected to
	// for the handle prompt; it POSTs the handle to MCPAuthorizeSubmitPath.
	MCPAuthorizePagePath   = "/ui/login/mcp"
	MCPAuthorizeSubmitPath = "/mcp/oauth/authorize/submit"
	MCPCallbackPath        = "/mcp/oauth/callback"
	MCPTokenPath           = "/mcp/oauth/token"
	MCPClientMetadataPath  = "/mcp/oauth/client-metadata.json"
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

// HandleMCPClientMetadata serves the atproto client metadata document that
// identifies this server to users' PDSes (the client_id URL) during the MCP
// handle-prompt login.
func (o *OAuthServer) HandleMCPClientMetadata(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(r.Context(), w, o.mcpClientMetadata)
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

// HandleMCPAuthorize validates the client's authorization request and
// redirects the browser to the pear-pages handle prompt (MCPAuthorizePagePath),
// the same way HandleAuthorize redirects to its own disambiguation page. That
// page POSTs the entered handle back to HandleMCPAuthorizeSubmit as JSON.
func (o *OAuthServer) HandleMCPAuthorize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ar, err := o.provider.NewAuthorizeRequest(ctx, r)
	if err != nil {
		o.provider.WriteAuthorizeError(ctx, w, ar, err)
		return
	}
	if resource := r.Form.Get("resource"); resource != "" && resource != o.mcpResource() {
		o.provider.WriteAuthorizeError(ctx, w, ar,
			fosite.ErrInvalidRequest.WithHint("The 'resource' parameter does not identify this MCP server."))
		return
	}
	if r.Form.Get("code_challenge") == "" || r.Form.Get("code_challenge_method") != "S256" {
		o.provider.WriteAuthorizeError(ctx, w, ar,
			fosite.ErrInvalidRequest.WithHint("PKCE with the S256 code challenge method is required."))
		return
	}
	requestID, err := randomToken(24)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("generate request id: %w", err))
		return
	}
	ar.SetSession(newSession())
	if err := o.storage.CreatePARSession(ctx, requestID, ar); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("save request: %w", err))
		return
	}
	clientName := ""
	if c, ok := ar.GetClient().(clientDisplay); ok {
		clientName = c.displayName()
	}
	page := url.Values{
		"request_id":  {requestID},
		"client_name": {clientName},
		"login_hint":  {r.Form.Get("login_hint")},
	}
	http.Redirect(w, r, MCPAuthorizePagePath+"?"+page.Encode(), http.StatusSeeOther)
}

// mcpAuthorizeSubmitRequest is the JSON body the pear-pages handle prompt
// POSTs to HandleMCPAuthorizeSubmit.
type mcpAuthorizeSubmitRequest struct {
	RequestID string `json:"requestId"`
	Handle    string `json:"handle"`
}

// HandleMCPAuthorizeSubmit starts the atproto login for the handle submitted
// from MCPAuthorizePagePath, and returns the URL to redirect the browser to.
func (o *OAuthServer) HandleMCPAuthorizeSubmit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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
	if _, err := o.storage.GetPARSession(ctx, req.RequestID); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "unknown or expired authorization request", err)
		return
	}
	redirect, state, err := o.mcpBroker.Start(ctx, handle)
	if err != nil {
		slog.WarnContext(ctx, "mcp oauth: failed to start atproto login", "handle", handle, "err", err)
		httpx.WriteError(
			ctx, w, "InvalidRequest", "Couldn't sign in with that handle. Check it and try again.",
			http.StatusBadRequest,
		)
		return
	}
	if err := o.storage.RekeyRequest(ctx, req.RequestID, state); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("save login state: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, map[string]string{"redirect": redirect})
}

// HandleMCPCallback receives the browser back from the user's PDS, completes
// the atproto login, and redirects to the MCP client with an authorization
// code.
func (o *OAuthServer) HandleMCPCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := r.URL.Query()
	state := query.Get("state")
	ar, err := o.storage.GetPARSession(ctx, state)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "unknown or expired login", err)
		return
	}
	login, err := o.mcpBroker.Finish(ctx, query)
	if err != nil {
		if delErr := o.storage.DeletePARSession(ctx, state); delErr != nil {
			slog.WarnContext(ctx, "mcp oauth: delete pending request", "err", delErr)
		}
		if errors.Is(err, ErrLoginDenied) {
			o.provider.WriteAuthorizeError(ctx, w, ar, fosite.ErrAccessDenied.WithHint("The login was not approved."))
			return
		}
		httpx.WriteServerError(ctx, w, fmt.Errorf("complete atproto login: %w", err))
		return
	}

	ar.SetSession(&session{
		Subject:  login.DID.String(),
		ClientID: ar.GetClient().GetID(),
		Scopes:   ar.GetRequestedScopes(),
	})
	o.finishAuthorize(ctx, w, state, ar, o.MCPIssuer())
}

// HandleMCPToken serves the MCP token endpoint (authorization_code and
// refresh_token grants). It shares OAuthServer's fosite.OAuth2Provider and
// storage with the atproto endpoints, so tokens minted here validate there
// and vice versa; the only difference is that MCP tokens are advertised as
// plain bearer tokens rather than the DPoP type atproto clients require (see
// HandleToken).
func (o *OAuthServer) HandleMCPToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	req, err := o.provider.NewAccessRequest(ctx, r, newSession())
	if err != nil {
		logError(ctx, err)
		o.provider.WriteAccessError(ctx, w, req, err)
		return
	}
	resp, err := o.provider.NewAccessResponse(ctx, req)
	if err != nil {
		logError(ctx, err)
		o.provider.WriteAccessError(ctx, w, req, err)
		return
	}
	resp.SetTokenType("Bearer")
	o.provider.WriteAccessResponse(ctx, w, req, resp)
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
