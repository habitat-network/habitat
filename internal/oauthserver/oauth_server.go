// Package oauthserver provides an OAuth 2.0 authorization server implementation
// that initiates a confidential client atproto OAuth flow
package oauthserver

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gorilla/sessions"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/node"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
	"github.com/ory/fosite/handler/oauth2"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"gorm.io/gorm"
)

const (
	// this is the cookie name.
	// TODO: hardcoding this means that only one oauth flow can be in progress at a time
	sessionName = "auth-session"
)

// authRequestFlash stores the authorization request state in a session flash.
// This data is temporarily stored during the OAuth authorization flow to preserve
// request context across redirects.
type authRequestFlash struct {
	Form           url.Values // Original authorization request form data
	DpopKey        []byte
	AuthorizeState *pdsclient.AuthorizeState // AT Protocol authorization state
	Did            syntax.DID                // DID of the user
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
	authorizeErrCtr, err := meter.Int64Counter("oauth.authorize.err", metric.WithUnit("Item"), metric.WithDescription("counts errors in OAuth /authorize implementation"))
	if err != nil {
		return nil, err
	}

	authorizeSuccessCtr, err := meter.Int64Counter("oauth.authorize.success", metric.WithUnit("Item"), metric.WithDescription("counts successes in OAuth /authorize implementation"))
	if err != nil {
		return nil, err
	}

	callbackErrCtr, err := meter.Int64Counter("oauth.callback.err", metric.WithUnit("Item"), metric.WithDescription("counts errors in OAuth /callback implementation"))
	if err != nil {
		return nil, err
	}

	callbackSuccessCtr, err := meter.Int64Counter("oauth.callback.success", metric.WithUnit("Item"), metric.WithDescription("counts successes in OAuth /callback implementation"))
	if err != nil {
		return nil, err
	}

	refreshTokenRequestCtr, err := meter.Int64Counter("oauth.refresh_token.request", metric.WithUnit("Item"), metric.WithDescription("counts request to refresh an OAuth token with habitat"))
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

func (m *metrics) authorizeErr(err error, reason string) {
	if errors.Is(err, context.Canceled) {
		return
	}
	m.authorizeErrCtr.Add(
		context.Background(),
		1,
		metric.WithAttributeSet(
			attribute.NewSet(
				attribute.String("reason", reason),
			),
		),
	)
}

func (m *metrics) authorizeSuccess() {
	m.authorizeSuccessCtr.Add(
		context.Background(),
		1,
	)
}

func (m *metrics) callbackErr(err error, reason string) {
	if errors.Is(err, context.Canceled) {
		return
	}
	m.callbackErrCtr.Add(
		context.Background(),
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

	// The habitat service name to look up in DID docs.
	node node.Node

	provider     fosite.OAuth2Provider
	credStore    pdscred.PDSCredentialStore // Database storage for OAuth sessions
	sessionStore sessions.Store             // Session storage for authorization flow state
	oauthClient  pdsclient.PdsOAuthClient   // Client for communicating with AT Protocol services
	directory    identity.Directory         // AT Protocol identity directory for handle resolution
	storage      *store
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
//   - oauthClient: Client for AT Protocol OAuth operations
//   - sessionStore: Store for managing user sessions during authorization flow
//   - directory: AT Protocol identity directory for resolving handles to DIDs
//   - db: GORM database connection for storing OAuth sessions
//   - credStore: Store for PDS credentials
//   - userStore: Store for managing users (can be nil if not needed)
//
// Returns a configured OAuthServer ready to handle authorization requests.
func NewOAuthServer(
	secret string,
	oauthClient pdsclient.PdsOAuthClient,
	sessionStore sessions.Store,
	node node.Node,
	directory identity.Directory,
	credStore pdscred.PDSCredentialStore,
	db *gorm.DB,
	meter metric.Meter,
) (*OAuthServer, error) {
	secretBytes, err := encrypt.ParseKey(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to parse secret: %w", err)
	}
	config := &fosite.Config{
		GlobalSecret:               secretBytes,
		SendDebugMessagesToClients: true,
		RefreshTokenScopes:         []string{},
	}
	strategy, err := newStrategy(secretBytes, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create strategy: %w", err)
	}
	storage, err := newStore(strategy, db)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage: %w", err)
	}
	// Register types for session serialization
	gob.Register(&authRequestFlash{})
	gob.Register(pdsclient.AuthorizeState{})

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
		credStore:    credStore,
		oauthClient:  oauthClient,
		sessionStore: sessionStore,
		directory:    directory,
		node:         node,
		storage:      storage,
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
		o.metrics.authorizeErr(err, fositeErrReason(err))
		o.provider.WriteAuthorizeError(ctx, w, requester, err)
		return
	}
	if err = r.ParseForm(); err != nil {
		o.metrics.authorizeErr(err, "parse_form")
		utils.LogAndHTTPError(w, err, "failed to parse form", http.StatusBadRequest)
		return
	}
	handle := r.Form.Get("handle")
	atid, err := syntax.ParseAtIdentifier(handle)
	if err != nil {
		o.metrics.authorizeErr(err, "parse_handle")
		utils.LogAndHTTPError(w, err, "failed to parse handle", http.StatusBadRequest)
		return
	}
	id, err := o.directory.Lookup(ctx, atid)
	if err != nil {
		o.metrics.authorizeErr(err, "lookup_atid")
		utils.LogAndHTTPError(w, err, "failed to lookup identity", http.StatusInternalServerError)
		return
	}
	dpopKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		o.metrics.authorizeErr(err, "gen_dpop_key")
		utils.LogAndHTTPError(w, err, "failed to generate key", http.StatusInternalServerError)
		return
	}
	dpopClient := pdsclient.NewDpopHttpClient(dpopKey, &pdsclient.MemoryNonceProvider{})
	redirect, state, err := o.oauthClient.Authorize(dpopClient, id)
	if err != nil {
		o.metrics.authorizeErr(err, fositeErrReason(err))
		utils.LogAndHTTPError(
			w,
			err,
			"failed to initiate authorization",
			http.StatusInternalServerError,
		)
		return
	}
	dpopKeyBytes, err := dpopKey.Bytes()
	if err != nil {
		o.metrics.authorizeErr(err, "serialize_dpop")
		utils.LogAndHTTPError(w, err, "failed to serialize key", http.StatusInternalServerError)
		return
	}
	authorizeSession, _ := o.sessionStore.New(r, sessionName)
	authorizeSession.AddFlash(&authRequestFlash{
		Form:           requester.GetRequestForm(),
		AuthorizeState: state,
		DpopKey:        dpopKeyBytes,
		Did:            id.DID,
	})
	if err := authorizeSession.Save(r, w); err != nil {
		o.metrics.authorizeErr(err, "save_flash")
		utils.LogAndHTTPError(w, err, "failed to save session", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, redirect, http.StatusSeeOther)
	o.metrics.authorizeSuccess()

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
	authorizeSession, err := o.sessionStore.Get(r, sessionName)
	if err != nil {
		o.metrics.callbackErr(err, "get_session")
		utils.LogAndHTTPError(w, err, "failed to get session", http.StatusInternalServerError)
		return
	}
	flashes := authorizeSession.Flashes()
	_ = authorizeSession.Save(r, w)
	if len(flashes) == 0 {
		o.metrics.callbackErr(err, "save_flash")
		utils.LogAndHTTPError(w, err, "failed to get auth request flash", http.StatusBadRequest)
		return
	}
	arf, ok := flashes[0].(*authRequestFlash)
	if !ok {
		o.metrics.callbackErr(err, "flash_type")
		utils.LogAndHTTPError(w, err, "failed to parse auth request flash", http.StatusBadRequest)
		return
	}
	recreatedRequest, err := http.NewRequest(http.MethodGet, "/?"+arf.Form.Encode(), nil)
	if err != nil {
		o.metrics.callbackErr(err, "recreate_req")
		utils.LogAndHTTPError(w, err, "failed to recreate request", http.StatusBadRequest)
		return
	}
	authRequest, err := o.provider.NewAuthorizeRequest(ctx, recreatedRequest)
	if err != nil {
		o.metrics.callbackErr(err, fositeErrReason(err))
		utils.LogAndHTTPError(w, err, "failed to recreate request", http.StatusBadRequest)
		return
	}
	dpopKey, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), arf.DpopKey)
	if err != nil {
		o.metrics.callbackErr(err, "parse_dpop")
		utils.LogAndHTTPError(w, err, "failed to parse dpop key", http.StatusBadRequest)
		return
	}
	dpopClient := pdsclient.NewDpopHttpClient(dpopKey, &pdsclient.MemoryNonceProvider{})
	tokenInfo, err := o.oauthClient.ExchangeCode(
		dpopClient,
		r.URL.Query().Get("code"),
		r.URL.Query().Get("iss"),
		arf.AuthorizeState,
	)
	if err != nil {
		o.metrics.callbackErr(err, fositeErrReason(err))
		utils.LogAndHTTPError(w, err, "failed to exchange code", http.StatusInternalServerError)
		return
	}

	// Store/update user credentials in the database with the PDS tokens
	err = o.credStore.UpsertCredentials(
		arf.Did,
		&pdscred.Credentials{
			AccessToken:  tokenInfo.AccessToken,
			RefreshToken: tokenInfo.RefreshToken,
			DpopKey:      dpopKey,
		},
	)
	if err != nil {
		o.metrics.callbackErr(err, "upsert_creds")
		utils.LogAndHTTPError(
			w,
			err,
			"failed to save user credentials",
			http.StatusInternalServerError,
		)
		return
	}

	if serves, err := o.node.ServesDID(r.Context(), arf.Did); err != nil {
		o.metrics.callbackErr(err, "lookup_serves")
		utils.LogAndHTTPError(w, err, "[oauth server: handle callback] failed to lookup did", http.StatusInternalServerError)
		return
	} else if !serves {
		o.metrics.callbackErr(err, "wrong_server")
		utils.LogAndHTTPError(
			w,
			err,
			"user's habitat service in DID doc does not match expected service",
			http.StatusMethodNotAllowed,
		)
		return
	}

	resp, err := o.provider.NewAuthorizeResponse(
		ctx,
		authRequest,
		newAuthorizeSession(authRequest, arf.Did),
	)
	if err != nil {
		o.metrics.callbackErr(err, fositeErrReason(err))
		utils.LogAndHTTPError(w, err, "failed to create response", http.StatusInternalServerError)
		return
	}
	o.provider.WriteAuthorizeResponse(r.Context(), w, authRequest, resp)
	o.metrics.callbackSuccess()
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
		o.provider.WriteAccessError(ctx, w, req, err)
		return
	}
	if req.GetGrantTypes().ExactOne("refresh_token") {
		o.metrics.refreshTokenRequestCtr.Add(context.Background(), 1)
	}
	resp, err := o.provider.NewAccessResponse(ctx, req)
	if err != nil {
		o.provider.WriteAccessError(ctx, w, req, err)
		return
	}
	o.provider.WriteAccessResponse(ctx, w, req, resp)
}

func (o *OAuthServer) HandleClientMetadata(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(o.oauthClient.ClientMetadata())
	if err != nil {
		utils.LogAndHTTPError(
			w,
			err,
			"failed to encode client metadata",
			http.StatusInternalServerError,
		)
		return
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
) (syntax.DID, bool) {
	token := fosite.AccessTokenFromRequest(r)
	did, ok, err := o.ValidateRaw(r.Context(), token, scopes...)
	if err != nil {
		// TODO: we should delegate the response to o.provider.WriteIntrospectionError(ctx, w, err)
		// Unfortunately that was returning a 200 http response, so we write our own error here.
		utils.WriteHTTPError(w, fmt.Errorf("unable to validate oauth token: %w", err), http.StatusUnauthorized)
	}
	return did, ok
}

// Validate's the given token and writes an error response to w if validation fails
func (o *OAuthServer) ValidateRaw(
	ctx context.Context,
	token string,
	scopes ...string,
) (syntax.DID, bool, error) {
	_, ar, err := o.provider.IntrospectToken(
		ctx,
		token,
		fosite.AccessToken,
		&oauth2.JWTSession{},
		scopes...,
	)
	if err != nil {
		return "", false, fmt.Errorf("invalid or expired token: %w", err)
	}
	// Get the DID from the session subject (stored in JWT)
	session := ar.GetSession().(*oauth2.JWTSession)
	if session.JWTClaims == nil {
		return "", false, fmt.Errorf("JWT claims not found")
	}

	did := session.JWTClaims.Subject
	if did == "" {
		return "", false, fmt.Errorf("DID not found in JWT")
	}
	return syntax.DID(did), true, nil
}

func (o *OAuthServer) ListConnectedApps(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := o.Validate(w, r)
	if !ok {
		return
	}

	var rows []ConnectedApp
	err := o.storage.db.WithContext(r.Context()).
		Where("subject = ?", callerDID).
		Find(&rows).Error
	if err != nil {
		utils.LogAndHTTPError(w, err, "listing connected apps", http.StatusInternalServerError)
		return
	}

	var output habitat.NetworkHabitatListConnectedAppsOutput
	output.Apps = make([]habitat.NetworkHabitatListConnectedAppsApp, len(rows))
	for i, row := range rows {
		fositeClient, err := o.storage.GetClient(r.Context(), row.ClientID)
		if err != nil {
			log.Warn().Err(err).Str("clientID", row.ClientID).Msg("failed to fetch client metadata")
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
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}
}
