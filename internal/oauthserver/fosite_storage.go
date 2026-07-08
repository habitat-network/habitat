package oauthserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v3"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/handler/pkce"
	"github.com/ory/fosite/handler/rfc7523"
	"github.com/ory/fosite/storage"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("github.com/habitat-network/habitat/internal/oauthserver")

type store struct {
	memoryStore              *storage.MemoryStore
	strategy                 *strategy
	db                       *gorm.DB
	approvedJwtBearerClients ApprovedClientStore
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
	strat *strategy,
	db *gorm.DB,
	approvedJwtBearerClients ApprovedClientStore,
) (*store, error) {
	err := db.AutoMigrate(&OAuthSession{}, &ConnectedApp{})
	if err != nil {
		return nil, err
	}
	// TODO: we need to add a goroutine here that cleans up expired sessions
	return &store{
		memoryStore:              storage.NewMemoryStore(),
		strategy:                 strat,
		db:                       db,
		approvedJwtBearerClients: approvedJwtBearerClients,
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

// CreatePARSession implements fosite.PARStorage. The pushed authorization
// request context is short-lived and single-use, so it is kept in memory
// rather than persisted; see the note on the memory store above.
func (s *store) CreatePARSession(
	ctx context.Context,
	requestURI string,
	request fosite.AuthorizeRequester,
) error {
	return s.memoryStore.CreatePARSession(ctx, requestURI, request)
}

// GetPARSession implements fosite.PARStorage.
func (s *store) GetPARSession(
	ctx context.Context,
	requestURI string,
) (fosite.AuthorizeRequester, error) {
	return s.memoryStore.GetPARSession(ctx, requestURI)
}

// DeletePARSession implements fosite.PARStorage.
func (s *store) DeletePARSession(ctx context.Context, requestURI string) error {
	return s.memoryStore.DeletePARSession(ctx, requestURI)
}

// ClientAssertionJWTValid implements fosite.Storage.
func (s *store) ClientAssertionJWTValid(ctx context.Context, jti string) error {
	return nil
}

// GetClient implements fosite.Storage.
func (s *store) GetClient(ctx context.Context, id string) (fosite.Client, error) {
	ctx, span := tracer.Start(ctx, "GetClient")
	defer span.End()
	span.SetAttributes(attribute.String("client_id", id))
	metadata, err := s.fetchClientMetadata(ctx, id)
	if err != nil {
		return nil, err
	}
	return &client{metadata}, nil
}

// fetchClientMetadata fetches and decodes the client metadata document
// published at id (the client's client_id URL). See
// https://atproto.com/specs/oauth#client-id-metadata-document.
func (s *store) fetchClientMetadata(
	ctx context.Context,
	id string,
) (*pdsclient.ClientMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, id, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	// TODO: consider caching
	cl := http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch client metadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch client metadata: status %d", resp.StatusCode)
	}

	var metadata pdsclient.ClientMetadata
	err = json.NewDecoder(resp.Body).Decode(&metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to decode client metadata: %w", err)
	}
	return &metadata, nil
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
	metadata, err := s.fetchClientMetadata(ctx, issuer)
	if err != nil {
		return nil, err
	}
	if metadata.Jwks == nil || len(metadata.Jwks.Keys) == 0 {
		return nil, fosite.ErrNotFound
	}
	return metadata.Jwks, nil
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

// IsJWTUsed implements rfc7523.RFC7523KeyStorage.
func (s *store) IsJWTUsed(ctx context.Context, jti string) (bool, error) {
	return false, nil
}

// MarkJWTUsedForTime implements rfc7523.RFC7523KeyStorage.
func (s *store) MarkJWTUsedForTime(ctx context.Context, jti string, exp time.Time) error {
	return nil
}

// SetClientAssertionJWT implements fosite.Storage.
func (s *store) SetClientAssertionJWT(ctx context.Context, jti string, exp time.Time) error {
	return s.memoryStore.SetClientAssertionJWT(ctx, jti, exp)
}

// CreateAuthorizeCodeSession implements oauth2.CoreStorage.
func (s *store) CreateAuthorizeCodeSession(
	ctx context.Context,
	code string,
	request fosite.Requester,
) (err error) {
	// Session data is encrypted in the code itself by the strategy
	return nil
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

	data, err := decodeSession(signature, s.strategy.encryptionKey)
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	client, err := s.GetClient(ctx, data.ClientID)
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	return &fosite.Request{
		Client:         client,
		Session:        data,
		RequestedScope: data.Scopes,
	}, nil
}

// InvalidateAuthorizeCodeSession implements oauth2.CoreStorage.
func (s *store) InvalidateAuthorizeCodeSession(ctx context.Context, code string) (err error) {
	// Stateless - code is self-contained and single-use
	return nil
}

// CreatePKCERequestSession implements pkce.PKCERequestStorage.
func (s *store) CreatePKCERequestSession(
	ctx context.Context,
	signature string,
	requester fosite.Requester,
) error {
	return nil
}

// DeletePKCERequestSession implements pkce.PKCERequestStorage.
func (s *store) DeletePKCERequestSession(ctx context.Context, signature string) error {
	return nil
}

// GetPKCERequestSession implements pkce.PKCERequestStorage.
func (s *store) GetPKCERequestSession(
	ctx context.Context,
	signature string,
	_ fosite.Session,
) (fosite.Requester, error) {
	data, err := decodeSession(signature, s.strategy.encryptionKey)
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	return &fosite.Request{
		Form: url.Values{
			"code_challenge":        []string{data.PKCEChallenge},
			"code_challenge_method": []string{"S256"},
		},
	}, nil
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
