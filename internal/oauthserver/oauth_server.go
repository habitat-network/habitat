// Package oauthserver provides an OAuth 2.0 authorization server implementation
// that initiates a confidential client atproto OAuth flow
package oauthserver

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/gorilla/sessions"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
	"github.com/ory/fosite/handler/oauth2"
	"go.opentelemetry.io/otel/metric"
	"gorm.io/gorm"
)

const (
	// this is the cookie name.
	// TODO: hardcoding this means that only one oauth flow can be in progress at a time
	sessionName = "auth-session"

	disambiguationPath = "/ui/login/disambiguate"
)

func init() {
	gob.Register(authRequestFlash{})
}

// authRequestFlash stores the authorization request state in a session flash.
// This data is temporarily stored during the OAuth authorization flow to preserve
// request context across redirects.
type authRequestFlash struct {
	Form          url.Values // Original authorization request form data
	ProviderState []byte     // Opaque provider-specific state
	Did           syntax.DID // DID of the user
}

// OAuthServer implements an OAuth 2.0 authorization server with AT Protocol integration.
// It handles OAuth authorization flows, token issuance, and integrates with DPoP
// for proof-of-possession token binding.
type OAuthServer struct {
	// Metrics
	metrics *metrics

	provider    fosite.OAuth2Provider
	loginRouter *org.LoginRouter   // Routes login flows by org login method
	directory   identity.Directory // AT Protocol identity directory for handle resolution
	storage     *store

	// Org store for membership lookups
	orgStore org.Store

	// Session store for OAuth flash data across redirects
	sessionStore sessions.Store

	// issuer origin (https URL, no path) the discovery metadata is built from.
	issuer string
}

// NewOAuthServer creates a new OAuth 2.0 authorization server instance.
//
// The server is configured with:
//   - Authorization Code Grant with PKCE
//   - Refresh Token Grant
//   - JWT Bearer Grant (RFC 7523) for a hardcoded allow-list of clients
//   - JWT token strategy for access tokens
//   - Integration with AT Protocol identity directory
//   - Database storage for OAuth sessions and PDS tokens
//
// Parameters:
//   - loginRouter: Routes login flows by DID service endpoint
//   - directory: AT Protocol identity directory for resolving handles to DIDs
//   - db: GORM database connection for storing OAuth sessions
//   - issuer: this server's issuer origin (an https URL with no path), from
//     which the endpoint URLs in the discovery metadata and the token endpoint
//     URL (used to validate the "aud" claim of JWT Bearer assertions) are built
//
// Returns a configured OAuthServer ready to handle authorization requests.
func NewOAuthServer(
	secret []byte,
	loginRouter *org.LoginRouter,
	directory identity.Directory,
	db *gorm.DB,
	meter metric.Meter,
	orgStore org.Store,
	issuer string,
	approvedJwtBearerClients ApprovedClientStore,
) (*OAuthServer, error) {
	config := &fosite.Config{
		GlobalSecret:               secret,
		SendDebugMessagesToClients: true,
		RefreshTokenScopes:         []string{},
		ScopeStrategy:              scopeStrategy,
		TokenURL:                   issuer + "/oauth/token",
		// The JWT Bearer grant identifies the client solely via the "iss"
		// claim of the assertion (checked against jwtBearerAllowedClients),
		// so a separate client_id/secret on the token request isn't required.
		GrantTypeJWTBearerCanSkipClientAuth: true,
		// TODO uncomment this when we migrate off openid-client
		// IsPushedAuthorizeEnforced:           true,
	}

	strategy, err := newStrategy(secret, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create strategy: %w", err)
	}
	storage, err := newStore(strategy, db, approvedJwtBearerClients)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage: %w", err)
	}

	oauthMetrics, err := newMetrics(meter)
	if err != nil {
		return nil, err
	}

	if loginRouter.OrgStore == nil {
		loginRouter.OrgStore = orgStore
	}

	cookieStore := sessions.NewCookieStore(secret)
	cookieStore.Options = &sessions.Options{
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
		MaxAge:   10 * 60,
	}

	return &OAuthServer{
		metrics: oauthMetrics,
		provider: compose.Compose(
			config,
			storage,
			strategy,
			compose.OAuth2AuthorizeExplicitFactory,
			compose.OAuth2RefreshTokenGrantFactory,
			compose.OAuth2PKCEFactory,
			compose.OAuth2StatelessJWTIntrospectionFactory, // Use stateless JWT introspection
			compose.RFC7523AssertionGrantFactory,
			compose.PushedAuthorizeHandlerFactory,
		),
		loginRouter:  loginRouter,
		sessionStore: cookieStore,
		directory:    directory,
		storage:      storage,
		orgStore:     orgStore,
		issuer:       issuer,
	}, nil
}

func fositeErrReason(err error) string {
	var rfcErr *fosite.RFC6749Error
	if errors.As(err, &rfcErr) {
		return rfcErr.ErrorField // "invalid_grant", "invalid_client", etc.
	}
	return "unknown"
}

// HandleAuthorize processes OAuth 2.0 authorization requests from the client.
//
// This handler initiates the authorization flow by:
//  1. Validating the client's authorize request
//  2. Resolving the user's atproto handle
//  3. Initiating authorization with the user's PDS
//  4. Storing the request context in a cookie session
//  5. Redirecting to the PDS for user authentication
//
// The request must include a "handle" form parameter with the user's handle
// (e.g., "alice.bsky.social").
//
// Returns an error if the authorization request is invalid or if any step in the
// authorization process fails. OAuth-specific errors are written directly to the
// response using the OAuth error format.
func (o *OAuthServer) HandleAuthorize(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx := r.Context()
	session, err := o.sessionStore.Get(r, sessionName)
	if err != nil {
		o.metrics.authorizeErr(ctx, err, "new_session")
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to create session",
			http.StatusInternalServerError,
		)
		return
	}
	flashes := session.Flashes("disambiguation")
	if err := session.Save(r, w); err != nil {
		o.metrics.authorizeErr(ctx, err, "save_session")
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to save session",
			http.StatusInternalServerError,
		)
		return
	}
	var requester fosite.AuthorizeRequester
	if len(flashes) > 0 {
		// resume request after disambiguation
		form := flashes[0].(authRequestFlash).Form
		form.Add("handle", r.URL.Query().Get("handle"))
		requester = &fosite.AuthorizeRequest{Request: fosite.Request{Form: form}}
	} else {
		requester, err = o.provider.NewAuthorizeRequest(ctx, r)
		if err != nil {
			o.metrics.authorizeErr(ctx, err, fositeErrReason(err))
			o.provider.WriteAuthorizeError(ctx, w, requester, err)
			return
		}
	}

	form := maps.Clone(requester.GetRequestForm())
	form.Del("request_uri")
	handle := form.Get("handle")
	if handle == "" {
		handle = form.Get("login_hint")
	}
	if handle == "" {
		slog.WarnContext(ctx, "request form", "form", requester.GetRequestForm())
		form.Del("handle")
		session.AddFlash(authRequestFlash{Form: form}, "disambiguation")
		if err := session.Save(r, w); err != nil {
			o.metrics.authorizeErr(ctx, err, "save_session")
			utils.LogAndHTTPError(
				ctx,
				w,
				err,
				"failed to save disambiguation session",
				http.StatusInternalServerError,
			)
		}
		http.Redirect(w, r, disambiguationPath, http.StatusSeeOther)
		return
	}

	atid, err := syntax.ParseAtIdentifier(handle)
	if err != nil {
		o.metrics.authorizeErr(ctx, err, "parse_handle")
		utils.LogAndHTTPError(ctx, w, err, "failed to parse handle", http.StatusBadRequest)
		return
	}

	// directory caches errors, so don't pass in the real context
	id, err := o.directory.Lookup(context.Background(), atid)
	if err != nil {
		o.metrics.authorizeErr(ctx, err, "lookup_atid")
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to lookup identity",
			http.StatusInternalServerError,
		)
		return
	}
	redirect, providerState, err := o.loginRouter.Authorize(ctx, id.DID)
	if err != nil {
		o.metrics.authorizeErr(ctx, err, "begin_login")
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to initiate authorization",
			http.StatusInternalServerError,
		)
		return
	}
	session.AddFlash(&authRequestFlash{
		Form:          requester.GetRequestForm(),
		ProviderState: providerState,
		Did:           id.DID,
	})
	if err := session.Save(r, w); err != nil {
		o.metrics.authorizeErr(ctx, err, "save_session")
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to save session",
			http.StatusInternalServerError,
		)
		return
	}

	http.Redirect(w, r, redirect, http.StatusSeeOther)
	o.metrics.authorizeSuccess(ctx)
}

// HandlePAR processes RFC 9126 Pushed Authorization Requests. The client POSTs
// the authorization request parameters (form-encoded) and receives a
// request_uri it can then hand to the authorization endpoint. PAR is supported
// but not required, so clients may also call /oauth/authorize directly.
func (o *OAuthServer) HandlePAR(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	loginHint := r.URL.Query().Get("handle")
	if loginHint == "" {
		loginHint = r.URL.Query().Get("login_hint")
	}
	if err := r.ParseForm(); err != nil {
		utils.LogAndHTTPError(ctx, w, err, "failed to parse form", http.StatusBadRequest)
		return
	}
	r.Form.Add("login_hint", loginHint)
	req, err := o.provider.NewPushedAuthorizeRequest(ctx, r)
	if err != nil {
		o.provider.WritePushedAuthorizeError(ctx, w, req, err)
		return
	}
	resp, err := o.provider.NewPushedAuthorizeResponse(ctx, req, newSession())
	if err != nil {
		o.provider.WritePushedAuthorizeError(ctx, w, req, err)
		return
	}
	o.provider.WritePushedAuthorizeResponse(ctx, w, req, resp)
}

// HandleCallback processes the OAuth callback from the user's PDS.
//
// This handler completes the authorization flow by:
//  1. Retrieving the stored authorization request context from the cookie session
//  2. Exchanging the authorization code for access and refresh tokens from the PDS
//  3. Storing the tokens in the user session
//  4. Generating an OAuth authorization response
//  5. Redirecting back to the original OAuth client with an authorization code
//
// The callback URL must include "code" and "iss" query parameters from the PDS.
//
// Returns an error if:
//   - The session is invalid or expired
//   - The authorization code exchange fails
//   - The response cannot be generated
func (o *OAuthServer) HandleCallback(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx := r.Context()
	session, err := o.sessionStore.Get(r, sessionName)
	if err != nil {
		o.metrics.callbackErr(ctx, err, "get_session")
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to get session",
			http.StatusBadRequest,
		)
		return
	}

	flashes := session.Flashes()
	if len(flashes) == 0 {
		o.metrics.callbackErr(ctx, nil, "no_flash")
		utils.LogAndHTTPError(
			ctx,
			w,
			nil,
			"no state found for session",
			http.StatusBadRequest,
		)
		return
	}
	arf, ok := flashes[0].(authRequestFlash)
	if !ok {
		o.metrics.callbackErr(ctx, nil, "invalid_flash_type")
		utils.LogAndHTTPError(
			ctx,
			w,
			nil,
			"invalid session data",
			http.StatusBadRequest,
		)
		return
	}

	session.Options.MaxAge = -1
	if err := session.Save(r, w); err != nil {
		o.metrics.callbackErr(ctx, err, "delete_session")
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to clear session",
			http.StatusInternalServerError,
		)
		return
	}
	// The stored form still carries the one-time PAR request_uri that fosite
	// already consumed when the flow began. Re-sending it would fail with
	// invalid_request_uri, so drop it and rebuild the authorize request from the
	// resolved parameters the form already contains.
	resumeForm := maps.Clone(arf.Form)
	resumeForm.Del("request_uri")
	recreatedRequest, err := http.NewRequest(http.MethodGet, "/?"+resumeForm.Encode(), nil)
	if err != nil {
		o.metrics.callbackErr(ctx, err, "recreate_req")
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to recreate request",
			http.StatusBadRequest,
		)
		return
	}
	authRequest, err := o.provider.NewAuthorizeRequest(ctx, recreatedRequest)
	if err != nil {
		o.metrics.callbackErr(ctx, err, fositeErrReason(err))
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to recreate request",
			http.StatusBadRequest,
		)
		return
	}

	if err := o.loginRouter.Exchange(
		ctx,
		arf.Did,
		r.URL.Query().Get("code"),
		r.URL.Query().Get("iss"),
		arf.ProviderState,
	); err != nil {
		o.metrics.callbackErr(ctx, err, "complete_login")
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to complete login",
			http.StatusInternalServerError,
		)
		return
	}

	// Grant the requested scopes so they are bound to the authorization code and
	// echoed back in the token response. Without this the token response carries
	// an empty scope, which atproto clients reject (they require a valid scope
	// containing "atproto"). The client's allowed scopes were already validated
	// when the authorize request was parsed.
	for _, scope := range authRequest.GetRequestedScopes() {
		authRequest.GrantScope(scope)
	}

	resp, err := o.provider.NewAuthorizeResponse(
		ctx,
		authRequest,
		newAuthorizeSession(authRequest, arf.Did),
	)
	if err != nil {
		o.metrics.callbackErr(ctx, err, fositeErrReason(err))
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to create response",
			http.StatusInternalServerError,
		)
		return
	}
	resp.AddParameter("iss", o.issuer)
	o.provider.WriteAuthorizeResponse(ctx, w, authRequest, resp)
	o.metrics.callbackSuccess()
}

// HandleToken processes OAuth 2.0 token requests from the client.
//
// This handler supports the following grant types:
//   - authorization_code: Exchange an authorization code for access and refresh tokens
//   - refresh_token: Use a refresh token to obtain a new access token
//   - urn:ietf:params:oauth:grant-type:jwt-bearer: Exchange a signed JWT assertion,
//     from a hardcoded allow-list of clients, for an access token
//
// The handler:
//  1. Validates the client's token request (client credentials, grant type, etc.)
//  2. Generates new access and refresh tokens
//  3. Returns the token response in JSON format
//
// Token requests must be POST requests with application/x-www-form-urlencoded content type
// and include the appropriate grant_type and credentials.
//
// Errors are written directly to the response using the OAuth error format.
func (o *OAuthServer) HandleToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
	ctx := r.Context()
	req, err := o.provider.NewAccessRequest(ctx, r, newSession())
	if err != nil {
		logError(ctx, err)
		o.provider.WriteAccessError(ctx, w, req, err)
		return
	}
	if req.GetGrantTypes().ExactOne("refresh_token") {
		o.metrics.refreshTokenRequestCtr.Add(context.Background(), 1)
	}
	resp, err := o.provider.NewAccessResponse(ctx, req)
	if err != nil {
		logError(ctx, err)
		o.provider.WriteAccessError(ctx, w, req, err)
		return
	}
	resp.SetExtra("sub", req.GetSession().GetSubject())
	// The atproto OAuth client requires DPoP-bound tokens and rejects any
	// token_type other than "DPoP". Habitat does not yet enforce DPoP
	// server-side (tokens remain bearer tokens in practice), but we advertise
	// the DPoP token type so atproto clients accept the response.
	// TODO: implement real DPoP proof validation and key binding.
	resp.SetTokenType("DPoP")
	o.provider.WriteAccessResponse(ctx, w, req, resp)
}

func logError(ctx context.Context, err error) {
	var rfcErr *fosite.RFC6749Error
	if errors.As(err, &rfcErr) {
		slog.ErrorContext(ctx, "token access error",
			"err", err,
			"error_field", rfcErr.ErrorField,
			"hint", rfcErr.HintField,
			"debug", rfcErr.DebugField,
		)
	} else {
		slog.ErrorContext(ctx, "token access error", "err", err)
	}
}

var _ authn.Method = (*OAuthServer)(nil)

func (o *OAuthServer) CanHandle(r *http.Request) bool {
	// The token may arrive under the Bearer or (from atproto clients) the DPoP
	// auth scheme. Strip either prefix before parsing, and fall back to the
	// Habitat-Auth-Method header if the token isn't a parseable oauth+JWT.
	tokenStr := r.Header.Get("Authorization")
	tokenStr = strings.TrimPrefix(tokenStr, "Bearer ")
	tokenStr = strings.TrimPrefix(tokenStr, "DPoP ")
	token, err := jwt.ParseSigned(tokenStr)
	if err == nil && token.Headers[0].ExtraHeaders["typ"] == "oauth+JWT" {
		return true
	}
	return r.Header.Get("Habitat-Auth-Method") == "oauth"
}

// Validate validates the given token and writes an error response to w if validation fails
func (o *OAuthServer) Validate(
	w http.ResponseWriter,
	r *http.Request,
	scopes ...string,
) (*authn.CredentialInfo, bool) {
	token := fosite.AccessTokenFromRequest(r)
	if token == "" {
		// The atproto OAuth client sends the access token with the DPoP auth
		// scheme ("Authorization: DPoP <token>"), which fosite's bearer-only
		// extractor ignores. Habitat does not enforce DPoP, so accept the token
		// regardless of scheme.
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "DPoP ") {
			token = strings.TrimPrefix(auth, "DPoP ")
		}
	}
	credInfo, ok, err := o.ValidateRaw(r.Context(), token, scopes...)
	if err != nil || !ok {
		// TODO: we should delegate the response to o.provider.WriteIntrospectionError(ctx, err)
		// Unfortunately that was returning a 200 http response, so we write our own error here.
		utils.WriteHTTPError(
			w,
			fmt.Errorf("unable to validate oauth token: %w", err),
			http.StatusUnauthorized,
		)
		return nil, false
	}

	return credInfo, true
}

// ValidateRaw validates the given token and writes an error response to w if validation fails
func (o *OAuthServer) ValidateRaw(
	ctx context.Context,
	token string,
	scopes ...string,
) (*authn.CredentialInfo, bool, error) {
	_, ar, err := o.provider.IntrospectToken(
		ctx,
		token,
		fosite.AccessToken,
		newSession(),
		scopes...,
	)
	if err != nil {
		return nil, false, fmt.Errorf("invalid or expired token: %w", err)
	}
	// Get the DID from the session subject (stored in JWT)
	session := ar.GetSession().(*oauth2.JWTSession)
	if session.JWTClaims == nil {
		return nil, false, fmt.Errorf("JWT claims not found")
	}

	did := session.JWTClaims.Subject
	if did == "" {
		return nil, false, fmt.Errorf("DID not found in JWT")
	}

	credInfo := &authn.CredentialInfo{Subject: syntax.DID(did)}

	org, isMember, err := o.orgStore.GetOrgForDID(ctx, syntax.DID(did))
	if err != nil {
		return nil, false, fmt.Errorf("failed to get org for DID: %w", err)
	}
	credInfo.Org = org
	if isMember {
		credInfo.Type = authn.UserCredential
	} else {
		credInfo.Type = authn.OrgCredential
	}

	return credInfo, true, nil
}

// HandleAuthServerMetadata serves the OAuth 2.0 Authorization Server Metadata
// document at /.well-known/oauth-authorization-server.
func (o *OAuthServer) HandleAuthServerMetadata(w http.ResponseWriter, r *http.Request) {
	writeMetadataJSON(r.Context(), w, buildAuthServerMetadata(o.issuer))
}

// HandleProtectedResourceMetadata serves the OAuth 2.0 Protected Resource
// Metadata document at /.well-known/oauth-protected-resource.
func (o *OAuthServer) HandleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	writeMetadataJSON(r.Context(), w, buildProtectedResourceMetadata(o.issuer))
}

func (o *OAuthServer) ListConnectedApps(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := o.Validate(w, r)
	if !ok {
		return
	}

	var rows []ConnectedApp
	err := o.storage.db.WithContext(r.Context()).
		Where("subject = ?", credInfo.Subject).
		Find(&rows).Error
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"listing connected apps",
			http.StatusInternalServerError,
		)
		return
	}

	var output habitat.NetworkHabitatListConnectedAppsOutput
	output.Apps = make([]habitat.NetworkHabitatListConnectedAppsApp, len(rows))
	for i, row := range rows {
		fositeClient, err := o.storage.GetClient(r.Context(), row.ClientID)
		if err != nil {
			slog.WarnContext(
				r.Context(),
				"failed to fetch client metadata",
				"err",
				err,
				"clientID",
				row.ClientID,
			)
			continue
		}

		c := fositeClient.(*client)
		output.Apps[i] = habitat.NetworkHabitatListConnectedAppsApp{
			ClientID:  row.ClientID,
			ClientUri: c.ClientUri,
			LastUsed:  row.UpdatedAt.Format(time.RFC3339Nano),
			Name:      c.ClientName,
			LogoUri:   c.LogoUri,
		}
	}
	httpx.WriteJSON(r.Context(), w, output)
}
