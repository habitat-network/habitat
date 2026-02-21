// Package oauthserver provides an OAuth 2.0 authorization server implementation
// that initiates a confidential client atproto OAuth flow
package oauthserver

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gorilla/sessions"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
	"github.com/ory/fosite/handler/oauth2"
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

// OAuthServer implements an OAuth 2.0 authorization server with AT Protocol integration.
// It handles OAuth authorization flows, token issuance, and integrates with DPoP
// for proof-of-possession token binding.
type OAuthServer struct {
	// The habitat service name to look up in DID docs.
	serviceName     string
	serviceEndpoint string

	provider     fosite.OAuth2Provider
	credStore    pdscred.PDSCredentialStore // Database storage for OAuth sessions
	sessionStore sessions.Store             // Session storage for authorization flow state
	oauthClient  pdsclient.PdsOAuthClient   // Client for communicating with AT Protocol services
	directory    identity.Directory         // AT Protocol identity directory for handle resolution
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
	serviceName string,
	serviceEndpoint string,
	secret string,
	oauthClient pdsclient.PdsOAuthClient,
	sessionStore sessions.Store,
	directory identity.Directory,
	credStore pdscred.PDSCredentialStore,
	db *gorm.DB,
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
	return &OAuthServer{
		serviceName:     serviceName,
		serviceEndpoint: serviceEndpoint,
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
	}, nil
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
		o.provider.WriteAuthorizeError(ctx, w, requester, err)
		return
	}
	if r.ParseForm() != nil {
		utils.LogAndHTTPError(w, err, "failed to parse form", http.StatusBadRequest)
		return
	}
	handle := r.Form.Get("handle")
	atid, err := syntax.ParseAtIdentifier(handle)
	if err != nil {
		utils.LogAndHTTPError(w, err, "failed to parse handle", http.StatusBadRequest)
		return
	}
	id, err := o.directory.Lookup(ctx, *atid)
	if err != nil {
		utils.LogAndHTTPError(w, err, "failed to lookup identity", http.StatusInternalServerError)
		return
	}
	dpopKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		utils.LogAndHTTPError(w, err, "failed to generate key", http.StatusInternalServerError)
		return
	}
	dpopClient := pdsclient.NewDpopHttpClient(dpopKey, &pdsclient.MemoryNonceProvider{})
	redirect, state, err := o.oauthClient.Authorize(dpopClient, id)
	if err != nil {
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
		utils.LogAndHTTPError(w, err, "failed to save session", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, redirect, http.StatusSeeOther)
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
		utils.LogAndHTTPError(w, err, "failed to get session", http.StatusInternalServerError)
		return
	}
	flashes := authorizeSession.Flashes()
	_ = authorizeSession.Save(r, w)
	if len(flashes) == 0 {
		utils.LogAndHTTPError(w, err, "failed to get auth request flash", http.StatusBadRequest)
		return
	}
	arf, ok := flashes[0].(*authRequestFlash)
	if !ok {
		utils.LogAndHTTPError(w, err, "failed to parse auth request flash", http.StatusBadRequest)
		return
	}
	recreatedRequest, err := http.NewRequest(http.MethodGet, "/?"+arf.Form.Encode(), nil)
	if err != nil {
		utils.LogAndHTTPError(w, err, "failed to recreate request", http.StatusBadRequest)
		return
	}
	authRequest, err := o.provider.NewAuthorizeRequest(ctx, recreatedRequest)
	if err != nil {
		utils.LogAndHTTPError(w, err, "failed to recreate request", http.StatusBadRequest)
		return
	}
	dpopKey, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), arf.DpopKey)
	if err != nil {
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
		utils.LogAndHTTPError(
			w,
			err,
			"failed to save user credentials",
			http.StatusInternalServerError,
		)
		return
	}

	// Ensure that habitat serves this user
	id, err := o.directory.LookupDID(ctx, arf.Did)
	if err != nil {
		utils.LogAndHTTPError(w, err, "failed to lookup did", http.StatusInternalServerError)
		return
	}

	if endpoint, ok := id.Services[o.serviceName]; !ok || endpoint.URL != o.serviceEndpoint {
		if err != nil {
			utils.LogAndHTTPError(
				w,
				err,
				"user's habitat service in DID doc does not match expected service",
				http.StatusInternalServerError,
			)
			return
		}
	}

	resp, err := o.provider.NewAuthorizeResponse(
		ctx,
		authRequest,
		newAuthorizeSession(authRequest, arf.Did),
	)
	if err != nil {
		utils.LogAndHTTPError(w, err, "failed to create response", http.StatusInternalServerError)
		return
	}
	o.provider.WriteAuthorizeResponse(r.Context(), w, authRequest, resp)
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
	_, ar, err := o.provider.IntrospectToken(
		r.Context(),
		fosite.AccessTokenFromRequest(r),
		fosite.AccessToken,
		&oauth2.JWTSession{},
		scopes...,
	)
	if err != nil {
		// TODO: we should delegate the response to o.provider.WriteIntrospectionError(ctx, w, err)
		// Unfortunately that was returning a 200 http response, so we write our own error here.
		utils.WriteHTTPError(
			w,
			fmt.Errorf("invalid or expired token: %w", err),
			http.StatusUnauthorized,
		)
		return "", false
	}
	// Get the DID from the session subject (stored in JWT)
	session := ar.GetSession().(*oauth2.JWTSession)
	if session.JWTClaims == nil {
		utils.LogAndHTTPError(
			w,
			fmt.Errorf("JWT claims not found"),
			"invalid token",
			http.StatusUnauthorized,
		)
		return "", false
	}

	did := session.JWTClaims.Subject
	if did == "" {
		utils.LogAndHTTPError(
			w,
			fmt.Errorf("DID not found in JWT"),
			"invalid token",
			http.StatusUnauthorized,
		)
		return "", false
	}
	return syntax.DID(did), true
}
