package oauthserver

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	jose "github.com/go-jose/go-jose/v3"
	"github.com/habitat-network/habitat/internal/clientmetadata"
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
	clientMeta               *clientmetadata.Resolver
}

// OAuthRequest is the single row type backing every short-lived piece of OAuth
// flow state: pushed authorization requests, in-flight authorization requests
// bridging the PDS redirect, and issued authorization codes. Which one a row is
// depends only on what its Key is (a PAR request_uri, a cookie-held request key,
// or an authorization code signature).
type OAuthRequest struct {
	Key      string `gorm:"primaryKey"`
	ClientID string `gorm:"size:1024"`
	// Subject is the resolved DID this flow authenticates. Empty until the login
	// hint is resolved (at PAR time) or the handle is resolved (at authorize time).
	Subject             string    `gorm:"size:255"`
	Scopes              string    `gorm:"size:512"` // space-separated
	CodeChallenge       string    `gorm:"size:255"`
	CodeChallengeMethod string    `gorm:"size:32"`
	RedirectURI         string    `gorm:"size:1024"`
	State               string    `gorm:"size:1024"` // the client's own OAuth state param
	ResponseType        string    `gorm:"size:64"`
	ResponseMode        string    `gorm:"size:32"`
	ExpiresAt           time.Time `gorm:"index"`
}

// toAuthorizeRequest rebuilds the fosite.AuthorizeRequest this row was stored
// from. The redirect URI is parsed rather than left in the form because
// WriteAuthorizeResponse reads the struct field, not the form.
func (r *OAuthRequest) toAuthorizeRequest(client fosite.Client) *fosite.AuthorizeRequest {
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

// fromRequester populates the OAuthRequest columns from a fosite.Requester.
func fromRequester(uri string, requester fosite.Requester, expiresAt time.Time) (r *OAuthRequest) {
	form := requester.GetRequestForm()
	return &OAuthRequest{
		Key:                 uri,
		ClientID:            requester.GetClient().GetID(),
		Scopes:              strings.Join(requester.GetRequestedScopes(), " "),
		CodeChallenge:       form.Get("code_challenge"),
		CodeChallengeMethod: form.Get("code_challenge_method"),
		RedirectURI:         form.Get("redirect_uri"),
		State:               form.Get("state"),
		ResponseType:        form.Get("response_type"),
		ResponseMode:        form.Get("response_mode"),
		ExpiresAt:           expiresAt,
		Subject:             requester.GetSession().GetSubject(),
	}
}

// pkceForm reconstructs the code_challenge/code_challenge_method form values
// for the PKCE handler from a stored request row.
func (r *OAuthRequest) pkceForm() url.Values {
	v := url.Values{}
	if r.CodeChallenge != "" {
		v.Set("code_challenge", r.CodeChallenge)
	}
	if r.CodeChallengeMethod != "" {
		v.Set("code_challenge_method", r.CodeChallengeMethod)
	}
	return v
}

type OAuthSession struct {
	Signature string `gorm:"primaryKey"`
	ClientID  string
	Subject   string // DID of the user
	Scopes    string // Space-separated scopes
	ExpiresAt time.Time
}

type ConnectedApp struct {
	Subject  string `gorm:"primaryKey,uniqueIndex:idx_connected_app"` // user DID
	ClientID string `gorm:"primaryKey,uniqueIndex:idx_connected_app"` // client_id URL
	Scopes   string // Space-separated scopes
	// GORM auto-managed
	CreatedAt time.Time
	UpdatedAt time.Time
}

func newStore(
	db *gorm.DB,
	approvedJwtBearerClients ApprovedClientStore,
	clientMeta *clientmetadata.Resolver,
) (*store, error) {
	err := db.AutoMigrate(&OAuthRequest{}, &OAuthSession{}, &ConnectedApp{})
	if err != nil {
		return nil, err
	}
	// TODO: we need to add a goroutine here that cleans up expired sessions
	return &store{
		db:                       db,
		approvedJwtBearerClients: approvedJwtBearerClients,
		clientMeta:               clientMeta,
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
// a fresh key while it bridges the PDS redirect; Save (upsert) rather than
// Create so re-storing the same key (e.g. once the login hint resolves to a
// DID) does not hit a duplicate-key error.
func (s *store) CreatePARSession(
	ctx context.Context,
	requestURI string,
	request fosite.AuthorizeRequester,
) error {
	return s.db.WithContext(ctx).
		Create(fromRequester(requestURI, request, time.Now().Add(parSessionTTL))).
		Error
}

// GetPARSession implements fosite.PARStorage.
func (s *store) GetPARSession(
	ctx context.Context,
	requestURI string,
) (fosite.AuthorizeRequester, error) {
	var r OAuthRequest
	err := s.db.WithContext(ctx).
		Where("expires_at > ?", time.Now()).
		First(&r, "key = ?", requestURI).
		Error
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	client, err := s.GetClient(ctx, r.ClientID)
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	return r.toAuthorizeRequest(client), nil
}

func (s *store) UpdatePARSessionSubject(
	ctx context.Context,
	requestURI string,
	subject syntax.DID,
) error {
	return s.db.WithContext(ctx).
		Model(&OAuthRequest{}).
		Where("key = ?", requestURI).
		Update("subject", subject).
		Error
}

// DeletePARSession implements fosite.PARStorage.
func (s *store) DeletePARSession(ctx context.Context, requestURI string) error {
	return s.db.WithContext(ctx).Delete(&OAuthRequest{}, "key = ?", requestURI).Error
}

// ClientAssertionJWTValid implements fosite.Storage. Client assertion JWTs are
// never checked (see SetClientAssertionJWT), so there is nothing to validate.
func (s *store) ClientAssertionJWTValid(ctx context.Context, jti string) error {
	return nil
}

// GetClient implements fosite.Storage.
func (s *store) GetClient(ctx context.Context, id string) (fosite.Client, error) {
	ctx, span := tracer.Start(ctx, "GetClient")
	defer span.End()
	span.SetAttributes(attribute.String("client_id", id))
	metadata, err := s.clientMeta.FetchMetadata(ctx, id)
	if err != nil {
		return nil, err
	}
	return &client{metadata}, nil
}

// GetPublicKey implements rfc7523.RFC7523KeyStorage. issuer is the "iss"
// claim of the JWT bearer assertion, expected to be a client ID (client
// metadata URL) present in the hardcoded jwtBearerAllowedClients allow-list.
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
	_ string,
) (*jose.JSONWebKeySet, error) {
	if !s.approvedJwtBearerClients.IsApprovedClient(issuer) {
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
	_ string,
	_ string,
) ([]string, error) {
	if !s.approvedJwtBearerClients.IsApprovedClient(issuer) {
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
	return s.db.WithContext(ctx).
		Create(
			fromRequester(
				signature,
				requester,
				requester.GetSession().GetExpiresAt(fosite.AuthorizeCode),
			),
		).Error
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

	var r OAuthRequest
	err = s.db.WithContext(ctx).
		Where("expires_at > ?", time.Now()).
		First(&r, "key = ?", signature).
		Error
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	client, err := s.GetClient(ctx, r.ClientID)
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	return r.toAuthorizeRequest(client), nil
}

// InvalidateAuthorizeCodeSession implements oauth2.CoreStorage.
func (s *store) InvalidateAuthorizeCodeSession(ctx context.Context, signature string) (err error) {
	return s.db.WithContext(ctx).Delete(&OAuthRequest{}, "key = ?", signature).Error
}

// CreatePKCERequestSession implements pkce.PKCERequestStorage. It writes the
// challenge onto the authorize code's row: this is the only point in the flow
// where fosite hands us the challenge un-sanitized.
func (s *store) CreatePKCERequestSession(
	ctx context.Context,
	signature string,
	requester fosite.Requester,
) error {
	form := requester.GetRequestForm()
	return s.db.WithContext(ctx).Model(&OAuthRequest{}).
		Where("key = ?", signature).
		Updates(map[string]any{
			"code_challenge":        form.Get("code_challenge"),
			"code_challenge_method": form.Get("code_challenge_method"),
		}).Error
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
	var r OAuthRequest
	if err := s.db.WithContext(ctx).First(&r, "key = ?", signature).Error; err != nil {
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
	oauthSession := &OAuthSession{
		Signature: signature,
		ClientID:  request.GetClient().GetID(),
		Subject:   sess.Subject,
		Scopes:    strings.Join(sess.Scopes, " "),
		ExpiresAt: sess.GetExpiresAt(fosite.RefreshToken),
	}

	err := s.db.WithContext(ctx).Create(oauthSession).Error
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
	return s.db.WithContext(ctx).Delete(&OAuthSession{}, "signature = ?", signature).Error
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

	var oauthSession OAuthSession

	err := s.db.WithContext(ctx).First(&oauthSession, "signature = ?", signature).Error
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
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
