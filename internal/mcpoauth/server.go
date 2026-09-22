// Package mcpoauth is the OAuth 2.1 authorization server for pear's MCP
// endpoint (internal/mcpserver). It is deliberately separate from
// internal/oauthserver, which serves atproto clients: MCP clients register
// dynamically (RFC 7591) rather than publishing a client metadata document,
// request their own scope, and receive tokens that are only valid for the MCP
// resource.
//
// Authorization prompts the user for a handle, then delegates to a Broker that
// runs atproto OAuth against that account's PDS. An account hosted by pear
// itself is simply redirected to pear's own atproto authorization server.
package mcpoauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
	fositeoauth2 "github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/token/hmac"
	"gorm.io/gorm"
)

// scopeMCP is the only scope this server grants. It carries no meaning of its
// own; MCP clients normally request none.
const scopeMCP = "mcp"

// Server is the MCP OAuth authorization server.
type Server struct {
	provider fosite.OAuth2Provider
	store    *store
	broker   Broker
	// issuer is the authorization server identifier (RFC 8414), an https URL
	// with a path: <origin>/mcp.
	issuer string
	// resource is the MCP resource access tokens are issued for.
	resource       string
	clientMetadata oauth.ClientMetadata
}

var _ authn.RawMethod = (*Server)(nil)

// Paths, relative to the origin, of the endpoints Server serves.
const (
	MetadataPath  = "/.well-known/oauth-authorization-server/mcp"
	RegisterPath  = "/mcp/oauth/register"
	AuthorizePath = "/mcp/oauth/authorize"
	// AuthorizePagePath is the pear-pages route the browser is redirected to
	// for the handle prompt; it POSTs the handle to AuthorizeSubmitPath.
	AuthorizePagePath   = "/ui/login/mcp"
	AuthorizeSubmitPath = "/mcp/oauth/authorize/submit"
	CallbackPath        = "/mcp/oauth/callback"
	TokenPath           = "/mcp/oauth/token"
	ClientMetadataPath  = "/mcp/oauth/client-metadata.json"
	// IssuerPath and ResourcePath are the paths of the issuer and MCP resource.
	IssuerPath   = "/mcp"
	ResourcePath = "/mcp"
)

// New returns the MCP authorization server for origin (an https URL with no
// path). secret must be the exact same root secret passed to
// internal/oauthserver.NewOAuthServer: both servers sign access tokens with
// the ECDSA key parsed directly from it (same derivation oauthserver uses),
// so the two issue interchangeable tokens — either server's token validates
// against either server's resources. Only the signing key needs to match;
// each server's own opaque grants (refresh tokens, authorization codes) stay
// private to it via its own HMAC key and storage.
func New(
	secret []byte,
	db *gorm.DB,
	broker Broker,
	clientMetadata oauth.ClientMetadata,
	origin string,
) (*Server, error) {
	privateKey, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), secret)
	if err != nil {
		return nil, fmt.Errorf("parse signing key: %w", err)
	}
	hmacKey, err := hkdf.Key(sha256.New, secret, nil, "habitat-mcp-oauth-hmac", 32)
	if err != nil {
		return nil, fmt.Errorf("derive hmac key: %w", err)
	}

	issuer := origin + IssuerPath
	resource := origin + ResourcePath
	st, err := newStore(db)
	if err != nil {
		return nil, err
	}

	config := &fosite.Config{
		GlobalSecret:                hmacKey,
		ScopeStrategy:               fosite.ExactScopeStrategy,
		EnforcePKCE:                 true,
		EnforcePKCEForPublicClients: true,
		// Issue refresh tokens regardless of requested scope: MCP clients
		// normally request none.
		RefreshTokenScopes: []string{},
		TokenURL:           issuer + "/oauth/token",
	}
	strategy := compose.NewOAuth2JWTStrategy(
		func(context.Context) (any, error) { return privateKey, nil },
		fositeoauth2.NewHMACSHAStrategy(&hmac.HMACStrategy{Config: config}, config),
		config,
	)
	provider := compose.Compose(
		config, st, strategy,
		compose.OAuth2AuthorizeExplicitFactory,
		compose.OAuth2RefreshTokenGrantFactory,
		compose.OAuth2PKCEFactory,
		compose.OAuth2StatelessJWTIntrospectionFactory,
	)
	return &Server{
		provider:       provider,
		store:          st,
		broker:         broker,
		issuer:         issuer,
		resource:       resource,
		clientMetadata: clientMetadata,
	}, nil
}

// Issuer returns the authorization server identifier.
func (s *Server) Issuer() string { return s.issuer }

// HandleMetadata serves RFC 8414 authorization server metadata.
func (s *Server) HandleMetadata(w http.ResponseWriter, r *http.Request) {
	origin := strings.TrimSuffix(s.issuer, IssuerPath)
	httpx.WriteJSON(r.Context(), w, map[string]any{
		"issuer":                                         s.issuer,
		"authorization_endpoint":                         origin + AuthorizePath,
		"token_endpoint":                                 origin + TokenPath,
		"registration_endpoint":                          origin + RegisterPath,
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"authorization_response_iss_parameter_supported": true,
	})
}

// HandleClientMetadata serves the atproto client metadata document that
// identifies this server to users' PDSes (the client_id URL).
func (s *Server) HandleClientMetadata(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(r.Context(), w, s.clientMetadata)
}

// registerRequest is the RFC 7591 registration request. Fields this server
// doesn't act on are ignored.
type registerRequest struct {
	RedirectURIs []string `json:"redirect_uris"`
	GrantTypes   []string `json:"grant_types"`
	ClientName   string   `json:"client_name"`
}

// HandleRegister implements RFC 7591 dynamic client registration. Registered
// clients are always public and must use PKCE.
func (s *Server) HandleRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req registerRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeOAuthError(ctx, w, "invalid_client_metadata", "failed to decode request body")
		return
	}
	if len(req.RedirectURIs) == 0 {
		writeOAuthError(ctx, w, "invalid_redirect_uri", "redirect_uris is required")
		return
	}
	for _, raw := range req.RedirectURIs {
		if err := validateRedirectURI(raw); err != nil {
			writeOAuthError(ctx, w, "invalid_redirect_uri", err.Error())
			return
		}
	}
	grantTypes := req.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code", "refresh_token"}
	}
	for _, g := range grantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			writeOAuthError(ctx, w, "invalid_client_metadata", "unsupported grant type "+g)
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
	if err := s.store.createClient(ctx, &Client{
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

// HandleAuthorize validates the client's authorization request and redirects
// the browser to the pear-pages handle prompt (AuthorizePagePath), the same
// way internal/oauthserver redirects to its own disambiguation page. That page
// POSTs the entered handle back to HandleAuthorizeSubmit as JSON.
func (s *Server) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ar, err := s.provider.NewAuthorizeRequest(ctx, r)
	if err != nil {
		s.provider.WriteAuthorizeError(ctx, w, ar, err)
		return
	}
	if resource := r.Form.Get("resource"); resource != "" && resource != s.resource {
		s.provider.WriteAuthorizeError(ctx, w, ar,
			fosite.ErrInvalidRequest.WithHint("The 'resource' parameter does not identify this MCP server."))
		return
	}
	if r.Form.Get("code_challenge") == "" || r.Form.Get("code_challenge_method") != "S256" {
		s.provider.WriteAuthorizeError(ctx, w, ar,
			fosite.ErrInvalidRequest.WithHint("PKCE with the S256 code challenge method is required."))
		return
	}
	requestID, err := randomToken(24)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("generate request id: %w", err))
		return
	}
	if err := s.store.createPending(ctx, requestID, ar); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("save request: %w", err))
		return
	}
	page := url.Values{
		"request_id":  {requestID},
		"client_name": {clientName(ctx, s.store, ar.GetClient().GetID())},
		"login_hint":  {r.Form.Get("login_hint")},
	}
	http.Redirect(w, r, AuthorizePagePath+"?"+page.Encode(), http.StatusSeeOther)
}

// authorizeSubmitRequest is the JSON body the pear-pages handle prompt POSTs
// to HandleAuthorizeSubmit.
type authorizeSubmitRequest struct {
	RequestID string `json:"requestId"`
	Handle    string `json:"handle"`
}

// HandleAuthorizeSubmit starts the atproto login for the handle submitted from
// AuthorizePagePath, and returns the URL to redirect the browser to.
func (s *Server) HandleAuthorizeSubmit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req authorizeSubmitRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid request body", err)
		return
	}
	handle := strings.TrimPrefix(strings.TrimSpace(req.Handle), "@")
	if handle == "" {
		httpx.WriteError(ctx, w, "InvalidRequest", "Enter your handle.", http.StatusBadRequest)
		return
	}
	if _, err := s.store.getPending(ctx, req.RequestID); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "unknown or expired authorization request", err)
		return
	}
	redirect, state, err := s.broker.Start(ctx, handle)
	if err != nil {
		slog.WarnContext(ctx, "mcp oauth: failed to start atproto login", "handle", handle, "err", err)
		httpx.WriteError(
			ctx, w, "InvalidRequest", "Couldn't sign in with that handle. Check it and try again.",
			http.StatusBadRequest,
		)
		return
	}
	if err := s.store.rekeyPending(ctx, req.RequestID, state); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("save login state: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, map[string]string{"redirect": redirect})
}

func clientName(ctx context.Context, st *store, clientID string) string {
	var row Client
	if err := st.db.WithContext(ctx).First(&row, "client_id = ?", clientID).Error; err != nil {
		return ""
	}
	return row.ClientName
}

// HandleCallback receives the browser back from the user's PDS, completes the
// atproto login, and redirects to the MCP client with an authorization code.
func (s *Server) HandleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := r.URL.Query()
	state := query.Get("state")
	ar, err := s.store.getPending(ctx, state)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "unknown or expired login", err)
		return
	}
	login, err := s.broker.Finish(ctx, query)
	if err != nil {
		if delErr := s.store.deletePending(ctx, state); delErr != nil {
			slog.WarnContext(ctx, "mcp oauth: delete pending request", "err", delErr)
		}
		if errors.Is(err, ErrLoginDenied) {
			s.provider.WriteAuthorizeError(ctx, w, ar, fosite.ErrAccessDenied.WithHint("The login was not approved."))
			return
		}
		httpx.WriteServerError(ctx, w, fmt.Errorf("complete atproto login: %w", err))
		return
	}

	for _, scope := range ar.GetRequestedScopes() {
		ar.GrantScope(scope)
	}
	resp, err := s.provider.NewAuthorizeResponse(ctx, ar, &session{
		Subject:  login.DID.String(),
		ClientID: ar.GetClient().GetID(),
		Scopes:   ar.GetRequestedScopes(),
	})
	if err != nil {
		s.provider.WriteAuthorizeError(ctx, w, ar, err)
		return
	}
	if err := s.store.deletePending(ctx, state); err != nil {
		slog.WarnContext(ctx, "mcp oauth: delete pending request", "err", err)
	}
	resp.AddParameter("iss", s.issuer)
	s.provider.WriteAuthorizeResponse(ctx, w, ar, resp)
}

// HandleToken serves the token endpoint (authorization_code and refresh_token
// grants).
func (s *Server) HandleToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	req, err := s.provider.NewAccessRequest(ctx, r, &session{})
	if err != nil {
		s.provider.WriteAccessError(ctx, w, req, err)
		return
	}
	resp, err := s.provider.NewAccessResponse(ctx, req)
	if err != nil {
		s.provider.WriteAccessError(ctx, w, req, err)
		return
	}
	resp.SetTokenType("Bearer")
	s.provider.WriteAccessResponse(ctx, w, req, resp)
}

// ValidateRaw implements authn.RawMethod: it validates an access token and
// returns the DID it was issued to. Tokens are signed with the same key as
// internal/oauthserver.OAuthServer (see New), so a token from either
// authorization server validates here, and a token minted here validates
// there — deliberately: see internal/oauthserver.OAuthServer.CanHandle.
func (s *Server) ValidateRaw(
	ctx context.Context, token string, scopes ...string,
) (*authn.CredentialInfo, bool, error) {
	_, ar, err := s.provider.IntrospectToken(ctx, token, fosite.AccessToken, &session{}, scopes...)
	if err != nil {
		return nil, false, fmt.Errorf("invalid or expired token: %w", err)
	}
	did := ar.GetSession().GetSubject()
	if did == "" {
		return nil, false, errors.New("token has no subject")
	}
	return &authn.CredentialInfo{Subject: syntaxDID(did)}, true, nil
}

func writeOAuthError(ctx context.Context, w http.ResponseWriter, code, description string) {
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

func syntaxDID(s string) syntax.DID { return syntax.DID(s) }
