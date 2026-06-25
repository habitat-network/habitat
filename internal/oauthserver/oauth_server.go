// Package oauthserver provides an OAuth 2.0 authorization server implementation
// that initiates a confidential client atproto OAuth flow
package oauthserver

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gorilla/sessions"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
	"github.com/ory/fosite/handler/oauth2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"gorm.io/gorm"
)

const (
	// this is the cookie name.
	// TODO: hardcoding this means that only one oauth flow can be in progress at a time
	sessionName = "auth-session"
	// sessionPath scopes the flash cookie to the callback endpoint that
	// consumes it. It must stay in sync between session creation and
	// invalidation, since a Set-Cookie only clears a cookie with a matching
	// path.
	sessionPath = "/oauth-callback"

	// flashMaxAge bounds how long an in-progress authorize flow may take
	// before its flash cookie expires.
	flashMaxAge = 10 * time.Minute
)

// authRequestFlash holds the authorization request state carried, as a
// one-shot flash, in the signed cookie session set by HandleAuthorize and
// consumed by HandleCallback, preserving request context across the redirect
// to the user's PDS and back.
type authRequestFlash struct {
	Form          url.Values // Original authorization request form data
	ProviderState []byte     // Opaque provider-specific state
	Did           syntax.DID // DID of the user
}

func init() {
	// Flash values are gob-encoded into the session cookie, so the concrete
	// type stored via AddFlash must be registered.
	gob.Register(authRequestFlash{})
}

type metrics struct {
	// HandleAuthorize
	authorizeErrCtr     metric.Int64Counter
	authorizeSuccessCtr metric.Int64Counter

	// HandleCallback
	callbackErrCtr     metric.Int64Counter
	callbackSuccessCtr metric.Int64Counter

	// HandleToken
	refreshTokenRequestCtr metric.Int64Counter
}

func newMetrics(meter metric.Meter) (*metrics, error) {
	authorizeErrCtr, err := meter.Int64Counter(
		"oauth.authorize.err",
		metric.WithUnit("Item"),
		metric.WithDescription("counts errors in OAuth /authorize implementation"),
	)
	if err != nil {
		return nil, err
	}

	authorizeSuccessCtr, err := meter.Int64Counter(
		"oauth.authorize.success",
		metric.WithUnit("Item"),
		metric.WithDescription("counts successes in OAuth /authorize implementation"),
	)
	if err != nil {
		return nil, err
	}

	callbackErrCtr, err := meter.Int64Counter(
		"oauth.callback.err",
		metric.WithUnit("Item"),
		metric.WithDescription("counts errors in OAuth /callback implementation"),
	)
	if err != nil {
		return nil, err
	}

	callbackSuccessCtr, err := meter.Int64Counter(
		"oauth.callback.success",
		metric.WithUnit("Item"),
		metric.WithDescription("counts successes in OAuth /callback implementation"),
	)
	if err != nil {
		return nil, err
	}

	refreshTokenRequestCtr, err := meter.Int64Counter(
		"oauth.refresh_token.request",
		metric.WithUnit("Item"),
		metric.WithDescription("counts request to refresh an OAuth token with habitat"),
	)
	if err != nil {
		return nil, err
	}

	return &metrics{
		authorizeErrCtr:        authorizeErrCtr,
		authorizeSuccessCtr:    authorizeSuccessCtr,
		callbackErrCtr:         callbackErrCtr,
		callbackSuccessCtr:     callbackSuccessCtr,
		refreshTokenRequestCtr: refreshTokenRequestCtr,
	}, nil
}

func (m *metrics) authorizeErr(ctx context.Context, err error, reason string) {
	if errors.Is(err, context.Canceled) {
		return
	}
	m.authorizeErrCtr.Add(
		ctx,
		1,
		metric.WithAttributeSet(
			attribute.NewSet(
				attribute.String("reason", reason),
			),
		),
	)
}

func (m *metrics) authorizeSuccess(ctx context.Context) {
	m.authorizeSuccessCtr.Add(ctx, 1)
}

func (m *metrics) callbackErr(ctx context.Context, err error, reason string) {
	if errors.Is(err, context.Canceled) {
		return
	}
	m.callbackErrCtr.Add(
		ctx,
		1,
		metric.WithAttributeSet(
			attribute.NewSet(
				attribute.String("reason", reason),
			),
		),
	)
}

func (m *metrics) callbackSuccess() {
	m.callbackSuccessCtr.Add(
		context.Background(),
		1,
	)
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

	// sessionStore carries short-lived OAuth flow state across redirects (the
	// pending authorize request between Authorize and Callback, and again
	// between Callback and the consent page for org DID logins) in signed,
	// encrypted cookies rather than server-side memory.
	sessionStore sessions.Store
}

// NewOAuthServer creates a new OAuth 2.0 authorization server instance.
//
// The server is configured with:
//   - Authorization Code Grant with PKCE
//   - Refresh Token Grant
//   - JWT token strategy for access tokens
//   - Integration with AT Protocol identity directory
//   - Database storage for OAuth sessions and PDS tokens
//
// Parameters:
//   - loginRouter: Routes login flows by DID service endpoint
//   - directory: AT Protocol identity directory for resolving handles to DIDs
//   - db: GORM database connection for storing OAuth sessions
//
// Returns a configured OAuthServer ready to handle authorization requests.
func NewOAuthServer(
	secret []byte,
	loginRouter *org.LoginRouter,
	directory identity.Directory,
	db *gorm.DB,
	meter metric.Meter,
	orgStore org.Store,
) (*OAuthServer, error) {
	config := &fosite.Config{
		GlobalSecret:               secret,
		SendDebugMessagesToClients: true,
		RefreshTokenScopes:         []string{},
		ScopeStrategy:              scopeStrategy,
	}

	strategy, err := newStrategy(secret, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create strategy: %w", err)
	}
	storage, err := newStore(strategy, db)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage: %w", err)
	}

	oauthMetrics, err := newMetrics(meter)
	if err != nil {
		return nil, err
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
		),
		loginRouter: loginRouter,
		// secret doubles as both the cookie authentication and encryption
		// key: it's already reused for several purposes in newStrategy, and
		// these cookies hold nothing beyond the lifetime of one OAuth flow.
		sessionStore: sessions.NewCookieStore(secret, secret),
		directory:    directory,
		storage:      storage,
		orgStore:     orgStore,
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
	requester, err := o.provider.NewAuthorizeRequest(ctx, r)
	if err != nil {
		o.metrics.authorizeErr(ctx, err, fositeErrReason(err))
		o.provider.WriteAuthorizeError(ctx, w, requester, err)
		return
	}
	if err = r.ParseForm(); err != nil {
		o.metrics.authorizeErr(ctx, err, "parse_form")
		utils.LogAndHTTPError(ctx, w, err, "failed to parse form", http.StatusBadRequest)
		return
	}
	handle := r.Form.Get("handle")
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

	// GetOrgForDID distinguishes an org DID (isMember false: an admin must
	// authenticate on the org's behalf) from a member DID (isMember true: the
	// member authenticates as themselves, identified to the provider by their
	// own login hint).
	targetOrg, isMember, err := o.orgStore.GetOrgForDID(ctx, id.DID)
	if err != nil {
		o.metrics.authorizeErr(ctx, err, "lookup_org")
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to look up organization",
			http.StatusUnauthorized,
		)
		return
	}

	loginHint := ""
	if isMember {
		member, err := o.orgStore.GetMember(ctx, id.DID)
		if err != nil {
			o.metrics.authorizeErr(ctx, err, "lookup_member")
			utils.LogAndHTTPError(
				ctx,
				w,
				err,
				"failed to look up member",
				http.StatusInternalServerError,
			)
			return
		}
		loginHint = member.LoginID
	}

	redirect, providerState, err := o.loginRouter.Authorize(ctx, targetOrg, loginHint)
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

	session, err := o.sessionStore.New(r, sessionName)
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
	session.Options = &sessions.Options{
		Path:     sessionPath,
		MaxAge:   int(flashMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
	}
	session.AddFlash(authRequestFlash{
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
	arf, err := o.popFlash(r, w)
	if err != nil {
		o.metrics.callbackErr(ctx, err, "no_flash")
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"no state found for session",
			http.StatusBadRequest,
		)
		return
	}

	authRequest, err := o.recreateAuthorizeRequest(ctx, arf.Form)
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

	// GetOrgForDID distinguishes an org DID (isMember false: an admin must
	// have completed the login, and the request requires explicit consent)
	// from a member DID (isMember true: the member must have completed the
	// login themselves).
	targetOrg, isMember, err := o.orgStore.GetOrgForDID(ctx, arf.Did)
	if err != nil {
		o.metrics.callbackErr(ctx, err, "lookup_org")
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to look up organization",
			http.StatusUnauthorized,
		)
		return
	}

	loginID, err := o.loginRouter.Exchange(
		ctx,
		targetOrg,
		r.URL.Query().Get("code"),
		r.URL.Query().Get("iss"),
		arf.ProviderState,
	)
	if err != nil {
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

	if err := o.verifyExchangedLogin(ctx, arf.Did, isMember, loginID); err != nil {
		o.metrics.callbackErr(ctx, err, "verify_login")
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to verify login",
			http.StatusUnauthorized,
		)
		return
	}

	if !isMember {
		o.beginConsent(w, r, arf.Form, arf.Did)
		return
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
	o.provider.WriteAuthorizeResponse(ctx, w, authRequest, resp)
	o.metrics.callbackSuccess()
}

// verifyExchangedLogin checks that loginID — the identity the login provider
// asserted completed the OAuth flow — is who was expected to log in for did:
// an admin of the org for an org-DID login (isMember false), or did's own
// registered member for a member-DID login (isMember true).
func (o *OAuthServer) verifyExchangedLogin(
	ctx context.Context,
	did syntax.DID,
	isMember bool,
	loginID string,
) error {
	if !isMember {
		member, err := o.orgStore.GetMemberByLoginID(ctx, loginID)
		if err != nil {
			return fmt.Errorf("failed to get member by login id: %w", err)
		}
		if member.Role != org.AdminRole {
			return fmt.Errorf("not an admin")
		}
		return nil
	}

	member, err := o.orgStore.GetMember(ctx, did)
	if err != nil {
		return fmt.Errorf("failed to get member: %w", err)
	}
	if member.LoginID != loginID {
		return fmt.Errorf("login id mismatch: %s != %s", member.LoginID, loginID)
	}
	return nil
}

// popFlash reads the flash cookie session set by HandleAuthorize and
// invalidates it, so it can't be replayed even if the rest of the callback
// fails partway through.
func (o *OAuthServer) popFlash(
	r *http.Request,
	w http.ResponseWriter,
) (*authRequestFlash, error) {
	session, err := o.sessionStore.Get(r, sessionName)
	if err != nil {
		return nil, err
	}

	flashes := session.Flashes()
	if len(flashes) == 0 {
		return nil, http.ErrNoCookie
	}
	arf, ok := flashes[0].(authRequestFlash)
	if !ok {
		return nil, http.ErrNoCookie
	}

	// Flashes() drops the value from the session; deleting the cookie too keeps
	// a spent flash cookie from lingering in the browser. Get() loads Options
	// from the store's defaults rather than whatever HandleAuthorize set
	// (Options aren't part of the encoded cookie), so Path must be set again
	// here: a Set-Cookie only clears a cookie when its path matches the original.
	session.Options.Path = sessionPath
	session.Options.MaxAge = -1
	if err := session.Save(r, w); err != nil {
		return nil, err
	}

	return &arf, nil
}

// recreateAuthorizeRequest rebuilds a fosite authorize request from the
// original request form data, e.g. data preserved in a flash across a
// redirect.
func (o *OAuthServer) recreateAuthorizeRequest(
	ctx context.Context,
	form url.Values,
) (fosite.AuthorizeRequester, error) {
	req, err := http.NewRequest(http.MethodGet, "/?"+form.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("recreate request: %w", err)
	}
	return o.provider.NewAuthorizeRequest(ctx, req)
}

// HandleToken processes OAuth 2.0 token requests from the client.
//
// This handler supports the following grant types:
//   - authorization_code: Exchange an authorization code for access and refresh tokens
//   - refresh_token: Use a refresh token to obtain a new access token
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
	req, err := o.provider.NewAccessRequest(ctx, r, &oauth2.JWTSession{})
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
	return r.Header.Get("Habitat-Auth-Method") == "oauth"
}

// Validate's the given token and writes an error response to w if validation fails
func (o *OAuthServer) Validate(
	w http.ResponseWriter,
	r *http.Request,
	scopes ...string,
) (*authn.CredentialInfo, bool) {
	token := fosite.AccessTokenFromRequest(r)
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

	_, isMember, err := o.orgStore.GetOrgForDID(r.Context(), credInfo.Subject)
	if err != nil {
		utils.WriteHTTPError(
			w,
			fmt.Errorf("not a member of this organization"),
			http.StatusUnauthorized,
		)
		return nil, false
	}

	if credInfo.Subject == "" {
		credInfo.Type = authn.InstanceCredential
	} else if isMember {
		credInfo.Type = authn.UserCredential
	} else {
		credInfo.Type = authn.OrgCredential
	}
	return credInfo, true
}

// Validate's the given token and writes an error response to w if validation fails
func (o *OAuthServer) ValidateRaw(
	ctx context.Context,
	token string,
	scopes ...string,
) (*authn.CredentialInfo, bool, error) {
	_, ar, err := o.provider.IntrospectToken(
		ctx,
		token,
		fosite.AccessToken,
		&oauth2.JWTSession{},
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
	return &authn.CredentialInfo{Subject: syntax.DID(did)}, true, nil
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
	err = json.NewEncoder(w).Encode(output)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"encoding response",
			http.StatusInternalServerError,
		)
		return
	}
}
