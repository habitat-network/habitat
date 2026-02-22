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

	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/handler/pkce"
	"github.com/ory/fosite/storage"
	"github.com/ory/fosite/token/jwt"
	"gorm.io/gorm"
)

type store struct {
	memoryStore *storage.MemoryStore
	strategy    *strategy
	db          *gorm.DB
}

type OAuthSession struct {
	Signature string `gorm:"primaryKey"`
	ClientID  string
	Subject   string // DID of the user
	Scopes    string // Space-separated scopes
	ExpiresAt time.Time
}

func newStore(strat *strategy, db *gorm.DB) (*store, error) {
	err := db.AutoMigrate(&OAuthSession{})
	if err != nil {
		return nil, err
	}
	// TODO: we need to add a goroutine here that cleans up expired sessions
	return &store{
		memoryStore: storage.NewMemoryStore(),
		strategy:    strat,
		db:          db,
	}, nil
}

var (
	_ fosite.Storage                = (*store)(nil)
	_ oauth2.CoreStorage            = (*store)(nil)
	_ oauth2.TokenRevocationStorage = (*store)(nil)
	_ pkce.PKCERequestStorage       = (*store)(nil)
)

// ClientAssertionJWTValid implements fosite.Storage.
func (s *store) ClientAssertionJWTValid(ctx context.Context, jti string) error {
	panic("not implemented")
}

// GetClient implements fosite.Storage.
func (s *store) GetClient(ctx context.Context, id string) (fosite.Client, error) {
	// TODO: consider caching
	resp, err := http.Get(id)
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
	return &client{metadata}, nil
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
	code string,
	session fosite.Session,
) (request fosite.Requester, err error) {
	var data authSession
	err = encrypt.DecryptCBOR(code, s.strategy.encryptionKey, &data)
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	client, err := s.GetClient(ctx, data.ClientID)
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	return &fosite.Request{
		Client:         client,
		Session:        newJWTSession(&data),
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
	_ context.Context,
	signature string,
	session fosite.Session,
) (fosite.Requester, error) {
	var data authSession
	err := encrypt.DecryptCBOR(signature, s.strategy.encryptionKey, &data)
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
	session := request.GetSession().(*oauth2.JWTSession)

	oauthSession := &OAuthSession{
		Signature: signature,
		ClientID:  request.GetClient().GetID(),
		Subject:   session.JWTClaims.Subject,
		Scopes:    strings.Join(session.JWTClaims.Scope, " "),
		ExpiresAt: session.GetExpiresAt(fosite.RefreshToken),
	}

	return s.db.WithContext(ctx).Create(oauthSession).Error
}

// DeleteRefreshTokenSession implements oauth2.CoreStorage.
func (s *store) DeleteRefreshTokenSession(ctx context.Context, signature string) error {
	return s.db.WithContext(ctx).Delete(&OAuthSession{}, "signature = ?", signature).Error
}

// GetRefreshTokenSession implements oauth2.CoreStorage.
func (s *store) GetRefreshTokenSession(
	ctx context.Context,
	signature string,
	session fosite.Session,
) (fosite.Requester, error) {
	var oauthSession OAuthSession
	err := s.db.WithContext(ctx).First(&oauthSession, "signature = ?", signature).Error
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}

	client, err := s.GetClient(ctx, oauthSession.ClientID)
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}

	scopes := fosite.Arguments{}
	if oauthSession.Scopes != "" {
		scopes = strings.Split(oauthSession.Scopes, " ")
	}

	jwtSession := &oauth2.JWTSession{
		JWTClaims: &jwt.JWTClaims{
			Subject:   oauthSession.Subject,
			ExpiresAt: oauthSession.ExpiresAt,
			Scope:     scopes,
		},
		JWTHeader: &jwt.Headers{},
	}

	return &fosite.Request{
		Client:         client,
		Session:        jwtSession,
		RequestedScope: scopes,
	}, nil
}

// RotateRefreshToken implements oauth2.CoreStorage.
func (s *store) RotateRefreshToken(
	ctx context.Context,
	requestID string,
	refreshTokenSignature string,
) (err error) {
	// Revoke the old refresh token by deleting it
	return s.DeleteRefreshTokenSession(ctx, refreshTokenSignature)
}

// RevokeRefreshToken implements oauth2.TokenRevocationStorage.
func (s *store) RevokeRefreshToken(_ context.Context, _ string) error {
	return fmt.Errorf("refresh token revocation not supported")
}
