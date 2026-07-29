// Package oauthserver provides an OAuth 2.0 authorization server implementation
// that initiates a confidential client atproto OAuth flow
package oauthserver

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
	"github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/token/hmac"
	"go.opentelemetry.io/otel/metric"
	"gorm.io/gorm"
)

const (
	// this is the cookie name.
	// TODO: hardcoding this means that only one oauth flow can be in progress at a time
	sessionName = "auth-session"

	disambiguationPath = "/ui/login/disambiguate"

	// requestKeyCookie holds the opaque key of the in-flight authorization
	// request row. It is what lets the callback find the request without
	// trusting the `state` the PDS echoes back — that state belongs to
	// pdsclient's own OAuth flow, not to ours.
	requestKeyCookie = "request_key"
	// providerStateCookie holds the opaque login-provider state for one redirect hop.
	providerStateCookie = "provider_state"
)

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

	// Cookie store for the request key and provider state across redirects
	cookieStore sessions.Store

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
	}

	privateKey, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), secret)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}
	strategy := compose.NewOAuth2JWTStrategy(func(ctx context.Context) (any, error) {
		return privateKey, nil
	}, oauth2.NewHMACSHAStrategy(&hmac.HMACStrategy{Config: config}, config), config)
	storage, err := newStore(db, approvedJwtBearerClients)
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
		cookieStore:  cookieStore,
		directory:    directory,
		storage:      storage,
		orgStore:     orgStore,
		issuer:       issuer,
	}, nil
}

func fositeErrReason(err error) string {
	if rfcErr, ok := errors.AsType[*fosite.RFC6749Error](err); ok {
		return rfcErr.ErrorField // "invalid_grant", "invalid_client", etc.
	}
	return "unknown"
}

// newRequestKey mints the opaque identifier for an in-flight authorization
// request. It is held only in the encrypted cookie, so it must be unguessable.
func newRequestKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate request key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
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
func (o *OAuthServer) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	cookie, err := o.cookieStore.Get(r, sessionName)
	if err != nil {
		o.metrics.authorizeErr(ctx, err, "get_cookie")
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to get cookie: %w", err))
		return
	}

	// staleKey is the row this request currently lives under, if any. It is
	// dropped once the request has been re-stored under a fresh key, so a
	// consumed PAR request_uri or a used disambiguation key cannot be replayed.
	var staleKey string
	var requester fosite.AuthorizeRequester
	switch resumeKey, _ := cookie.Values[requestKeyCookie].(string); {
	case resumeKey != "":
		staleKey = resumeKey
		requester, err = o.storage.GetPARSession(ctx, resumeKey)
		if err != nil {
			o.metrics.authorizeErr(ctx, err, "resume_lookup")
			httpx.WriteInvalidRequest(ctx, w, "authorization request expired", err)
			return
		}
	case r.URL.Query().Get("request_uri") != "":
		staleKey = r.URL.Query().Get("request_uri")
		requester, err = o.storage.GetPARSession(ctx, staleKey)
		if err != nil {
			o.metrics.authorizeErr(ctx, err, "par_lookup")
			o.provider.WriteAuthorizeError(ctx, w, requester, fosite.ErrInvalidRequestURI)
			return
		}
	default:
		requester, err = o.provider.NewAuthorizeRequest(ctx, r)
		if err != nil {
			o.metrics.authorizeErr(ctx, err, fositeErrReason(err))
			o.provider.WriteAuthorizeError(ctx, w, requester, err)
			return
		}
	}

	key, err := newRequestKey()
	if err != nil {
		o.metrics.authorizeErr(ctx, err, "new_request_key")
		httpx.WriteServerError(ctx, w, err)
		return
	}

	// HandlePAR resolves the login hint at push time, so the subject may already
	// be known; otherwise fall back to a handle from the disambiguation redirect
	// or the request form.
	var did syntax.DID
	var subject string
	if sess := requester.GetSession(); sess != nil {
		subject = sess.GetSubject()
	}
	if subject != "" {
		did, err = syntax.ParseDID(subject)
		if err != nil {
			o.metrics.authorizeErr(ctx, err, "parse_subject")
			httpx.WriteServerError(ctx, w, fmt.Errorf("failed to parse stored subject: %w", err))
			return
		}
	} else {
		handle := r.URL.Query().Get("handle")
		if handle == "" {
			handle = requester.GetRequestForm().Get("handle")
		}
		if handle == "" {
			handle = requester.GetRequestForm().Get("login_hint")
		}
		if handle == "" {
			// Park the request and let the user pick an identity. The cookie
			// carries only the key, so the resumed request is provably ours.
			if err := o.storage.CreateAuthorizeFlowSession(ctx, key, requester, ""); err != nil {
				o.metrics.authorizeErr(ctx, err, "store_disambiguation")
				httpx.WriteServerError(ctx, w, fmt.Errorf("failed to store request: %w", err))
				return
			}
			if staleKey != "" {
				if err := o.storage.DeletePARSession(ctx, staleKey); err != nil {
					slog.WarnContext(ctx, "failed to delete stale oauth request", "err", err)
				}
			}
			cookie.Values[requestKeyCookie] = key
			delete(cookie.Values, providerStateCookie)
			if err := cookie.Save(r, w); err != nil {
				o.metrics.authorizeErr(ctx, err, "save_cookie")
				httpx.WriteServerError(ctx, w, fmt.Errorf("failed to save cookie: %w", err))
				return
			}
			http.Redirect(w, r, disambiguationPath, http.StatusSeeOther)
			return
		}

		atid, err := syntax.ParseAtIdentifier(handle)
		if err != nil {
			o.metrics.authorizeErr(ctx, err, "parse_handle")
			httpx.WriteInvalidRequest(ctx, w, "failed to parse handle", err)
			return
		}
		// directory caches errors, so don't pass in the real context
		id, err := o.directory.Lookup(context.Background(), atid)
		if err != nil {
			o.metrics.authorizeErr(ctx, err, "lookup_atid")
			httpx.WriteServerError(ctx, w, fmt.Errorf("failed to lookup atid: %w", err))
			return
		}
		did = id.DID
	}

	redirect, providerState, err := o.loginRouter.Authorize(ctx, did)
	if err != nil {
		o.metrics.authorizeErr(ctx, err, "begin_login")
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to begin login: %w", err))
		return
	}

	if err := o.storage.CreateAuthorizeFlowSession(ctx, key, requester, did); err != nil {
		o.metrics.authorizeErr(ctx, err, "store_request")
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to store request: %w", err))
		return
	}
	if staleKey != "" {
		if err := o.storage.DeletePARSession(ctx, staleKey); err != nil {
			slog.WarnContext(ctx, "failed to delete stale oauth request", "err", err)
		}
	}

	cookie.Values[requestKeyCookie] = key
	cookie.Values[providerStateCookie] = providerState
	if err := cookie.Save(r, w); err != nil {
		o.metrics.authorizeErr(ctx, err, "save_cookie")
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to save cookie: %w", err))
		return
	}

	// Redirect to the provider untouched: pdsclient already put its own state
	// inside the request it pushed to the PDS, and appending ours would be both
	// ignored and ambiguous on the way back.
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
		httpx.WriteInvalidRequest(ctx, w, "failed to parse form", err)
		return
	}
	r.Form.Add("login_hint", loginHint)
	req, err := o.provider.NewPushedAuthorizeRequest(ctx, r)
	if err != nil {
		o.provider.WritePushedAuthorizeError(ctx, w, req, err)
		return
	}
	sess := newSession()
	if loginHint != "" {
		if atid, err := syntax.ParseAtIdentifier(loginHint); err == nil {
			if id, err := o.directory.Lookup(context.Background(), atid); err == nil {
				sess.Subject = id.DID.String()
			} else {
				slog.WarnContext(ctx, "failed to resolve PAR login hint", "err", err, "hint", loginHint)
			}
		}
	}
	resp, err := o.provider.NewPushedAuthorizeResponse(ctx, req, sess)
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
func (o *OAuthServer) HandleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	cookie, err := o.cookieStore.Get(r, sessionName)
	if err != nil {
		o.metrics.callbackErr(ctx, err, "get_cookie")
		httpx.WriteInvalidRequest(ctx, w, "failed to get cookie", err)
		return
	}
	key, _ := cookie.Values[requestKeyCookie].(string)
	providerState, _ := cookie.Values[providerStateCookie].([]byte)

	// The cookie is single-use: clear it before doing anything that can fail.
	cookie.Options.MaxAge = -1
	if err := cookie.Save(r, w); err != nil {
		o.metrics.callbackErr(ctx, err, "delete_cookie")
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to delete cookie: %w", err))
		return
	}
	if key == "" {
		o.metrics.callbackErr(ctx, nil, "no_cookie_state")
		httpx.WriteInvalidRequest(ctx, w, "no authorization request in progress", nil)
		return
	}

	requester, err := o.storage.GetPARSession(ctx, key)
	if err != nil {
		o.metrics.callbackErr(ctx, err, "request_lookup")
		httpx.WriteInvalidRequest(ctx, w, "authorization request expired", err)
		return
	}
	defer func() {
		if err := o.storage.DeletePARSession(ctx, key); err != nil {
			slog.WarnContext(ctx, "failed to delete oauth request", "err", err)
		}
	}()

	did, err := syntax.ParseDID(requester.GetSession().GetSubject())
	if err != nil {
		o.metrics.callbackErr(ctx, err, "parse_did")
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to parse stored subject: %w", err))
		return
	}

	if err := o.loginRouter.Exchange(ctx, did, r.URL.Query(), providerState); err != nil {
		o.metrics.callbackErr(ctx, err, "complete_login")
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to complete login: %w", err))
		return
	}

	// Grant the requested scopes so they are bound to the authorization code and
	// echoed back in the token response. Without this the token response carries
	// an empty scope, which atproto clients reject (they require a valid scope
	// containing "atproto"). The client's allowed scopes were already validated
	// when the authorize request was parsed.
	for _, scope := range requester.GetRequestedScopes() {
		requester.GrantScope(scope)
	}

	resp, err := o.provider.NewAuthorizeResponse(ctx, requester, &session{
		Subject:       did.String(),
		ClientID:      requester.GetClient().GetID(),
		Scopes:        requester.GetRequestedScopes(),
		PKCEChallenge: requester.GetRequestForm().Get("code_challenge"),
	})
	if err != nil {
		o.metrics.callbackErr(ctx, err, fositeErrReason(err))
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to create response: %w", err))
		return
	}
	resp.AddParameter("iss", o.issuer)
	o.provider.WriteAuthorizeResponse(ctx, w, requester, resp)
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
	if rfcErr, ok := errors.AsType[*fosite.RFC6749Error](err); ok {
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
	token, err := jwt.ParseSigned(tokenFromRequest(r))
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
	ctx := r.Context()
	token := tokenFromRequest(r)
	credInfo, ok, err := o.ValidateRaw(ctx, token, scopes...)
	if err != nil || !ok {
		// TODO: we should delegate the response to o.provider.WriteIntrospectionError(ctx, err)
		// Unfortunately that was returning a 200 http response, so we write our own error here.
		slog.WarnContext(ctx, "invalid token", "err", err)
		httpx.WriteError(ctx, w, "Unauthorized", "", http.StatusUnauthorized)
		return nil, false
	}

	return credInfo, true
}

func tokenFromRequest(r *http.Request) string {
	// The token may arrive under the Bearer or (from atproto clients) the DPoP
	// auth scheme. Strip either prefix before parsing, and fall back to the
	// Habitat-Auth-Method header if the token isn't a parseable oauth+JWT.
	tokenStr := r.Header.Get("Authorization")
	tokenStr = strings.TrimPrefix(tokenStr, "Bearer ")
	tokenStr = strings.TrimPrefix(tokenStr, "DPoP ")
	return tokenStr
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
	httpx.WriteJSON(r.Context(), w, buildAuthServerMetadata(o.issuer))
}

// HandleProtectedResourceMetadata serves the OAuth 2.0 Protected Resource
// Metadata document at /.well-known/oauth-protected-resource.
func (o *OAuthServer) HandleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(r.Context(), w, buildProtectedResourceMetadata(o.issuer))
}

func (o *OAuthServer) ListConnectedApps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := o.Validate(w, r)
	if !ok {
		return
	}

	var rows []ConnectedApp
	err := o.storage.db.WithContext(ctx).
		Where("subject = ?", credInfo.Subject).
		Find(&rows).Error
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to list connected apps: %w", err))
		return
	}

	var output habitat.NetworkHabitatListConnectedAppsOutput
	output.Apps = make([]habitat.NetworkHabitatListConnectedAppsApp, len(rows))
	for i, row := range rows {
		fositeClient, err := o.storage.GetClient(ctx, row.ClientID)
		if err != nil {
			slog.WarnContext(
				ctx,
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
	httpx.WriteJSON(ctx, w, output)
}
