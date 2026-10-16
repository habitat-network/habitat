package oauthserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	jose "github.com/go-jose/go-jose/v3"
	"github.com/habitat-network/habitat/internal/clientmetadata"
	"github.com/habitat-network/habitat/internal/keyval"
	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/handler/pkce"
	"github.com/ory/fosite/handler/rfc7523"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("github.com/habitat-network/habitat/internal/oauthserver")

type store struct {
	db                       *gorm.DB
	approvedJwtBearerClients ApprovedClientStore
	appAccess                appAccessChecker
	clientMeta               *clientmetadata.Resolver
	// kv holds the short-lived OAuth flow state (pushed authorization requests,
	// in-flight requests, authorization codes). It must already be namespaced.
	kv keyval.Store
}

// appAccessChecker reports whether an org has approved an app (see
// opensocial.Store.HasAppAccess).
type appAccessChecker interface {
	HasAppAccess(ctx context.Context, orgDID syntax.DID, clientID string) (bool, error)
}

// jwtBearerClientAllowed reports whether issuer may use the JWT bearer grant
// to act as subject: either it is a builtin client, or subject is an org
// that has an app access record for it.
func (s *store) jwtBearerClientAllowed(
	ctx context.Context,
	issuer string,
	subject string,
) (bool, error) {
	if s.approvedJwtBearerClients.IsApprovedClient(issuer) {
		return true, nil
	}
	did, err := syntax.ParseDID(subject)
	if err != nil {
		return false, nil
	}
	return s.appAccess.HasAppAccess(ctx, did, issuer)
}

// requestRecord is the serialized value of one in-flight OAuth flow: pushed
// authorization requests, requests bridging the PDS redirect, and issued
// authorization codes. fosite types are not serialized directly because they
// embed a Client and Session whose unexported state does not survive a JSON
// round trip.
type requestRecord struct {
	ClientID string `json:"client_id"`
	// Subject is the resolved DID this flow authenticates. Empty until the login
	// hint is resolved (at PAR time) or the handle is resolved (at authorize time).
	Subject             string `json:"subject"`
	Scopes              string `json:"scopes"` // space-separated
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	RedirectURI         string `json:"redirect_uri"`
	State               string `json:"state"` // the client's own OAuth state param
	ResponseType        string `json:"response_type"`
	ResponseMode        string `json:"response_mode"`
}

// toAuthorizeRequest rebuilds the fosite.AuthorizeRequest this row was stored
// from. The redirect URI is parsed rather than left in the form because
// WriteAuthorizeResponse reads the struct field, not the form.
func (r *requestRecord) toAuthorizeRequest(client fosite.Client) *fosite.AuthorizeRequest {
	scopes := fosite.Arguments(strings.Fields(r.Scopes))
	var redirectURI *url.URL
	redirectURI, _ = url.Parse(r.RedirectURI)
	return &fosite.AuthorizeRequest{
		ResponseTypes:        fosite.Arguments(strings.Fields(r.ResponseType)),
		ResponseMode:         fosite.ResponseModeType(r.ResponseMode),
		HandledResponseTypes: fosite.Arguments{},
		RedirectURI:          redirectURI,
		State:                r.State,
		Request: fosite.Request{
			Client: client,
			Session: &session{
				Subject:  r.Subject,
				ClientID: r.ClientID,
				Scopes:   scopes,
			},
			RequestedScope: scopes,
			GrantedScope:   scopes,
			Form: url.Values{
				"code_challenge":        {r.CodeChallenge},
				"code_challenge_method": {r.CodeChallengeMethod},
			},
			RequestedAt: time.Now().UTC(),
		},
	}
}

// requestRecordFrom populates a requestRecord from a fosite.Requester.
func requestRecordFrom(requester fosite.Requester) requestRecord {
	form := requester.GetRequestForm()
	return requestRecord{
		ClientID:            requester.GetClient().GetID(),
		Scopes:              strings.Join(requester.GetRequestedScopes(), " "),
		CodeChallenge:       form.Get("code_challenge"),
		CodeChallengeMethod: form.Get("code_challenge_method"),
		RedirectURI:         form.Get("redirect_uri"),
		State:               form.Get("state"),
		ResponseType:        form.Get("response_type"),
		ResponseMode:        form.Get("response_mode"),
		Subject:             requester.GetSession().GetSubject(),
	}
}

// pkceForm reconstructs the code_challenge/code_challenge_method form values
// for the PKCE handler from a stored request record.
func (r *requestRecord) pkceForm() url.Values {
	v := url.Values{}
	if r.CodeChallenge != "" {
		v.Set("code_challenge", r.CodeChallenge)
	}
	if r.CodeChallengeMethod != "" {
		v.Set("code_challenge_method", r.CodeChallengeMethod)
	}
	return v
}

// refreshRecord is what a refresh token's keyval entry holds: enough to
// rebuild the session the token endpoint reads back. The entry expires with the
// token, so ExpiresAt is only echoed back to fosite.
type refreshRecord struct {
	ClientID  string    `json:"client_id"`
	Subject   string    `json:"subject"`
	Scopes    string    `json:"scopes"` // space-separated
	ExpiresAt time.Time `json:"expires_at"`
}

// refreshKey namespaces refresh token signatures among the other entries in the
// OAuth server's keyval namespace, which are keyed by authorize code signature
// and PAR request URI.
func refreshKey(signature string) string { return "refresh/" + signature }

type ConnectedApp struct {
	Subject  string `gorm:"primaryKey,uniqueIndex:idx_connected_app"` // user DID
	ClientID string `gorm:"primaryKey,uniqueIndex:idx_connected_app"` // client_id URL
	Scopes   string // Space-separated scopes
	// GORM auto-managed
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RegisteredClient is an OAuth client registered through RFC 7591 dynamic
// client registration (the MCP endpoints; see HandleMCPRegister). Clients
// authenticated via an atproto client-id metadata document are never stored
// here — they're resolved live by GetClient instead.
type RegisteredClient struct {
	ClientID     string `gorm:"primaryKey"`
	ClientName   string
	RedirectURIs []byte // JSON []string
	GrantTypes   []byte // JSON []string
	CreatedAt    time.Time
}

func newStore(
	db *gorm.DB,
	approvedJwtBearerClients ApprovedClientStore,
	appAccess appAccessChecker,
	clientMeta *clientmetadata.Resolver,
	kv keyval.Store,
) (*store, error) {
	return &store{
		db:                       db,
		approvedJwtBearerClients: approvedJwtBearerClients,
		appAccess:                appAccess,
		clientMeta:               clientMeta,
		kv:                       kv,
	}, nil
}

var (
	_ fosite.Storage                = (*store)(nil)
	_ fosite.PARStorage             = (*store)(nil)
	_ oauth2.CoreStorage            = (*store)(nil)
	_ oauth2.TokenRevocationStorage = (*store)(nil)
	_ pkce.PKCERequestStorage       = (*store)(nil)
	_ rfc7523.RFC7523KeyStorage     = (*store)(nil)
)

// parSessionTTL bounds how long an in-flight authorization request — pushed or
// mid-PDS-redirect — stays resumable.
const parSessionTTL = 10 * time.Minute

// CreatePARSession implements fosite.PARStorage. It also doubles as the
// re-store step the authorize flow uses to persist an in-flight request under
// a fresh key while it bridges the PDS redirect; Put upserts, so re-storing the
// same key (e.g. once the login hint resolves to a DID) is not an error.
func (s *store) CreatePARSession(
	ctx context.Context,
	requestURI string,
	request fosite.AuthorizeRequester,
) error {
	return s.kv.Put(ctx, requestURI, requestRecordFrom(request), parSessionTTL)
}

// GetPARSession implements fosite.PARStorage.
func (s *store) GetPARSession(
	ctx context.Context,
	requestURI string,
) (fosite.AuthorizeRequester, error) {
	return s.getAuthorizeRequest(ctx, requestURI)
}

// getAuthorizeRequest rebuilds the AuthorizeRequest stored under key, as a
// PAR session or an authorization code.
func (s *store) getAuthorizeRequest(
	ctx context.Context,
	key string,
) (*fosite.AuthorizeRequest, error) {
	var r requestRecord
	if err := s.kv.Get(ctx, key, &r); err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	client, err := s.GetClient(ctx, r.ClientID)
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	return r.toAuthorizeRequest(client), nil
}

// UpdatePARSessionSubject rewrites the subject on a stored PAR session. A
// missing or expired session is a no-op, as the row update it replaces was. The
// entry gets a fresh parSessionTTL: the store cannot report the remaining TTL,
// and this runs at most once, moments after the session was created.
func (s *store) UpdatePARSessionSubject(
	ctx context.Context,
	requestURI string,
	subject syntax.DID,
) error {
	var r requestRecord
	if err := s.kv.Get(ctx, requestURI, &r); err != nil {
		if errors.Is(err, keyval.ErrNotFound) {
			return nil
		}
		return err
	}
	r.Subject = subject.String()
	return s.kv.Put(ctx, requestURI, r, parSessionTTL)
}

// DeletePARSession implements fosite.PARStorage. Deleting a session that is
// already gone is not an error.
func (s *store) DeletePARSession(ctx context.Context, requestURI string) error {
	return ignoreNotFound(s.kv.Delete(ctx, requestURI))
}

// ignoreNotFound treats an absent entry as already deleted.
func ignoreNotFound(err error) error {
	if errors.Is(err, keyval.ErrNotFound) {
		return nil
	}
	return err
}

// ClientAssertionJWTValid implements fosite.Storage. Client assertion JWTs are
// never checked (see SetClientAssertionJWT), so there is nothing to validate.
func (s *store) ClientAssertionJWTValid(ctx context.Context, jti string) error {
	return nil
}

// GetClient implements fosite.Storage. It first checks for a dynamically
// registered client (RFC 7591, used by the MCP endpoints), then falls back to
// resolving id as an atproto client-id metadata document URL.
func (s *store) GetClient(ctx context.Context, id string) (fosite.Client, error) {
	ctx, span := tracer.Start(ctx, "GetClient")
	defer span.End()
	span.SetAttributes(attribute.String("client_id", id))

	var row RegisteredClient
	err := s.db.WithContext(ctx).First(&row, "client_id = ?", id).Error
	switch {
	case err == nil:
		dc := &dynamicClient{RegisteredClient: &row}
		if err := json.Unmarshal(row.RedirectURIs, &dc.redirectURIs); err != nil {
			return nil, fmt.Errorf("decode redirect uris: %w", err)
		}
		if err := json.Unmarshal(row.GrantTypes, &dc.grantTypes); err != nil {
			return nil, fmt.Errorf("decode grant types: %w", err)
		}
		return dc, nil
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, err
	}

	metadata, err := s.clientMeta.FetchMetadata(ctx, id)
	if err != nil {
		return nil, err
	}
	return &client{metadata}, nil
}

// createRegisteredClient persists a client registered through RFC 7591
// dynamic client registration.
func (s *store) createRegisteredClient(ctx context.Context, c *RegisteredClient) error {
	return s.db.WithContext(ctx).Create(c).Error
}

// GetPublicKey implements rfc7523.RFC7523KeyStorage. issuer is the "iss"
// claim of the JWT bearer assertion, expected to be a client ID (client
// metadata URL) that is on the builtin allow-list or that the subject org has
// granted app access to (see jwtBearerClientAllowed).
func (s *store) GetPublicKey(
	ctx context.Context,
	issuer string,
	subject string,
	keyID string,
) (*jose.JSONWebKey, error) {
	keys, err := s.GetPublicKeys(ctx, issuer, subject)
	if err != nil {
		return nil, err
	}
	for _, key := range keys.Keys {
		if key.KeyID == keyID {
			return &key, nil
		}
	}
	return nil, fosite.ErrNotFound
}

// GetPublicKeys implements rfc7523.RFC7523KeyStorage.
func (s *store) GetPublicKeys(
	ctx context.Context,
	issuer string,
	subject string,
) (*jose.JSONWebKeySet, error) {
	allowed, err := s.jwtBearerClientAllowed(ctx, issuer, subject)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, fosite.ErrNotFound
	}
	metadata, err := s.clientMeta.FetchMetadata(ctx, issuer)
	if err != nil {
		return nil, err
	}
	if metadata.JWKS == nil || len(metadata.JWKS.Keys) == 0 {
		return nil, fosite.ErrNotFound
	}
	var keys []jose.JSONWebKey
	for _, key := range metadata.JWKS.Keys {
		if key.KeyID == nil {
			continue
		}
		converted, err := clientmetadata.ConvertJWK(key)
		if err != nil {
			continue
		}
		keys = append(keys, *converted)
	}
	if len(keys) == 0 {
		return nil, fosite.ErrNotFound
	}
	return &jose.JSONWebKeySet{
		Keys: keys,
	}, nil
}

// GetPublicKeyScopes implements rfc7523.RFC7523KeyStorage.
func (s *store) GetPublicKeyScopes(
	ctx context.Context,
	issuer string,
	subject string,
	_ string,
) ([]string, error) {
	allowed, err := s.jwtBearerClientAllowed(ctx, issuer, subject)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, fosite.ErrNotFound
	}
	cl, err := s.GetClient(ctx, issuer)
	if err != nil {
		return nil, err
	}
	return cl.GetScopes(), nil
}

// IsJWTUsed implements rfc7523.RFC7523KeyStorage. Assertion JTIs are never
// tracked (see SetClientAssertionJWT), so none are ever considered used.
func (s *store) IsJWTUsed(ctx context.Context, jti string) (bool, error) {
	return false, nil
}

// MarkJWTUsedForTime implements rfc7523.RFC7523KeyStorage.
func (s *store) MarkJWTUsedForTime(ctx context.Context, jti string, exp time.Time) error {
	return nil
}

// SetClientAssertionJWT implements fosite.Storage. Client assertion JWTs are
// never checked by this server (see ClientAssertionJWTValid), so tracking
// them is unnecessary.
func (s *store) SetClientAssertionJWT(ctx context.Context, jti string, exp time.Time) error {
	return nil
}

// CreateAuthorizeCodeSession implements oauth2.CoreStorage.
func (s *store) CreateAuthorizeCodeSession(
	ctx context.Context,
	signature string,
	requester fosite.Requester,
) (err error) {
	ctx, span := tracer.Start(ctx, "CreateAuthorizeCodeSession")
	defer span.End()
	span.SetAttributes(attribute.String("auth_signature", signature))
	return s.kv.Put(
		ctx,
		signature,
		requestRecordFrom(requester),
		time.Until(requester.GetSession().GetExpiresAt(fosite.AuthorizeCode)),
	)
}

// GetAuthorizeCodeSession implements oauth2.CoreStorage.
func (s *store) GetAuthorizeCodeSession(
	ctx context.Context,
	signature string,
	_ fosite.Session,
) (request fosite.Requester, err error) {
	ctx, span := tracer.Start(ctx, "GetAuthorizeCodeSession")
	defer span.End()
	span.SetAttributes(attribute.String("auth_signature", signature))

	return s.getAuthorizeRequest(ctx, signature)
}

// InvalidateAuthorizeCodeSession implements oauth2.CoreStorage.
func (s *store) InvalidateAuthorizeCodeSession(ctx context.Context, signature string) (err error) {
	return ignoreNotFound(s.kv.Delete(ctx, signature))
}

// CreatePKCERequestSession implements pkce.PKCERequestStorage. It writes the
// challenge onto the authorize code's row: this is the only point in the flow
// where fosite hands us the challenge un-sanitized.
func (s *store) CreatePKCERequestSession(
	ctx context.Context,
	signature string,
	requester fosite.Requester,
) error {
	var r requestRecord
	if err := s.kv.Get(ctx, signature, &r); err != nil {
		if errors.Is(err, keyval.ErrNotFound) {
			return nil
		}
		return err
	}
	form := requester.GetRequestForm()
	r.CodeChallenge = form.Get("code_challenge")
	r.CodeChallengeMethod = form.Get("code_challenge_method")
	// Re-put with the authorize code's own lifetime so the update does not
	// extend it.
	return s.kv.Put(
		ctx,
		signature,
		r,
		time.Until(requester.GetSession().GetExpiresAt(fosite.AuthorizeCode)),
	)
}

// DeletePKCERequestSession implements pkce.PKCERequestStorage. The challenge
// lives on the authorize code's row, which InvalidateAuthorizeCodeSession
// already deletes.
func (s *store) DeletePKCERequestSession(ctx context.Context, signature string) error {
	return nil
}

// GetPKCERequestSession implements pkce.PKCERequestStorage. The client is
// populated because pkce.validateNoPKCE calls IsPublic() on it.
func (s *store) GetPKCERequestSession(
	ctx context.Context,
	signature string,
	_ fosite.Session,
) (fosite.Requester, error) {
	var r requestRecord
	if err := s.kv.Get(ctx, signature, &r); err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	client, err := s.GetClient(ctx, r.ClientID)
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	return &fosite.Request{Client: client, Form: r.pkceForm()}, nil
}

// CreateAccessTokenSession implements oauth2.CoreStorage.
func (s *store) CreateAccessTokenSession(
	_ context.Context,
	_ string,
	_ fosite.Requester,
) (err error) {
	return nil
}

// GetAccessTokenSession implements oauth2.CoreStorage.
func (s *store) GetAccessTokenSession(
	ctx context.Context,
	signature string,
	session fosite.Session,
) (fosite.Requester, error) {
	return &fosite.Request{Session: session}, nil
}

// DeleteAccessTokenSession implements oauth2.CoreStorage.
func (s *store) DeleteAccessTokenSession(_ context.Context, _ string) error {
	return nil
}

// RevokeAccessToken implements oauth2.TokenRevocationStorage.
func (s *store) RevokeAccessToken(ctx context.Context, requestID string) error {
	return fmt.Errorf("access token revocation not supported")
}

// CreateRefreshTokenSession implements oauth2.CoreStorage.
func (s *store) CreateRefreshTokenSession(
	ctx context.Context,
	signature string,
	accessSignature string,
	request fosite.Requester,
) error {
	ctx, span := tracer.Start(ctx, "CreateRefreshTokenSession")
	defer span.End()
	span.SetAttributes(
		attribute.String("refresh_signature", signature),
		attribute.String("access_signature", accessSignature),
		attribute.String("client_id", request.GetClient().GetID()),
	)

	sess := request.GetSession().(*session)
	oauthSession := refreshRecord{
		ClientID:  request.GetClient().GetID(),
		Subject:   sess.Subject,
		Scopes:    strings.Join(sess.Scopes, " "),
		ExpiresAt: sess.GetExpiresAt(fosite.RefreshToken),
	}

	err := s.kv.Put(
		ctx,
		refreshKey(signature),
		oauthSession,
		time.Until(oauthSession.ExpiresAt),
	)
	if err != nil {
		return err
	}

	return s.db.WithContext(ctx).
		Where(ConnectedApp{Subject: oauthSession.Subject, ClientID: oauthSession.ClientID}).
		Assign(ConnectedApp{Scopes: oauthSession.Scopes}).
		FirstOrCreate(&ConnectedApp{}).Error
}

// DeleteRefreshTokenSession implements oauth2.CoreStorage.
func (s *store) DeleteRefreshTokenSession(ctx context.Context, signature string) error {
	ctx, span := tracer.Start(ctx, "DeleteRefreshTokenSession")
	defer span.End()
	span.SetAttributes(attribute.String("refresh_signature", signature))
	return ignoreNotFound(s.kv.Delete(ctx, refreshKey(signature)))
}

// GetRefreshTokenSession implements oauth2.CoreStorage.
func (s *store) GetRefreshTokenSession(
	ctx context.Context,
	signature string,
	_ fosite.Session,
) (fosite.Requester, error) {
	ctx, span := tracer.Start(ctx, "GetRefreshTokenSession")
	defer span.End()
	span.SetAttributes(attribute.String("refresh_signature", signature))

	var oauthSession refreshRecord
	if err := s.kv.Get(ctx, refreshKey(signature), &oauthSession); err != nil {
		if errors.Is(err, keyval.ErrNotFound) {
			return nil, errors.Join(fosite.ErrNotFound, err)
		}
		return nil, err
	}

	client, err := s.GetClient(ctx, oauthSession.ClientID)
	if err != nil {
		return nil, err
	}

	scopes := fosite.Arguments{}
	if oauthSession.Scopes != "" {
		scopes = strings.Split(oauthSession.Scopes, " ")
	}

	return &fosite.Request{
		Client: client,
		Session: &session{
			Subject:               oauthSession.Subject,
			ClientID:              oauthSession.ClientID,
			Scopes:                scopes,
			RefreshTokenExpiresAt: oauthSession.ExpiresAt,
		},
		RequestedScope: scopes,
		GrantedScope:   scopes,
	}, nil
}

// RotateRefreshToken implements oauth2.CoreStorage.
func (s *store) RotateRefreshToken(
	ctx context.Context,
	requestID string,
	refreshTokenSignature string,
) (err error) {
	ctx, span := tracer.Start(ctx, "RotateRefreshToken")
	defer span.End()
	span.SetAttributes(
		attribute.String("request_id", requestID),
		attribute.String("refresh_signature", refreshTokenSignature),
	)
	return s.DeleteRefreshTokenSession(ctx, refreshTokenSignature)
}

// RevokeRefreshToken implements oauth2.TokenRevocationStorage.
func (s *store) RevokeRefreshToken(_ context.Context, _ string) error {
	return fmt.Errorf("refresh token revocation not supported")
}

// Models returns the GORM models this package persists. Their tables are
// created by db.Migrate.
func Models() []any {
	return []any{&ConnectedApp{}, &RegisteredClient{}}
}
