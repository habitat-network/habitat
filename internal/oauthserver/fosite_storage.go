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

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/habitat-network/habitat/internal/encrypt"
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
	Subject   string
	Scopes    string
	ExpiresAt time.Time
}

type ConnectedApp struct {
	Subject  string `gorm:"primaryKey,uniqueIndex:idx_connected_app"`
	ClientID string `gorm:"primaryKey,uniqueIndex:idx_connected_app"`
	Scopes   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func newStore(strat *strategy, db *gorm.DB) (*store, error) {
	err := db.AutoMigrate(&OAuthSession{}, &ConnectedApp{})
	if err != nil {
		return nil, err
	}
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

func (s *store) ClientAssertionJWTValid(ctx context.Context, jti string) error {
	panic("not implemented")
}

func (s *store) GetClient(ctx context.Context, id string) (fosite.Client, error) {
	resp, err := http.Get(id)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch client metadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch client metadata: status %d", resp.StatusCode)
	}

	var metadata oauth.ClientMetadata
	err = json.NewDecoder(resp.Body).Decode(&metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to decode client metadata: %w", err)
	}
	return &client{metadata}, nil
}

func (s *store) SetClientAssertionJWT(ctx context.Context, jti string, exp time.Time) error {
	return s.memoryStore.SetClientAssertionJWT(ctx, jti, exp)
}

func (s *store) CreateAuthorizeCodeSession(
	ctx context.Context,
	code string,
	request fosite.Requester,
) (err error) {
	return nil
}

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

func (s *store) InvalidateAuthorizeCodeSession(ctx context.Context, code string) (err error) {
	return nil
}

func (s *store) CreatePKCERequestSession(
	ctx context.Context,
	signature string,
	requester fosite.Requester,
) error {
	return nil
}

func (s *store) DeletePKCERequestSession(ctx context.Context, signature string) error {
	return nil
}

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

func (s *store) CreateAccessTokenSession(
	_ context.Context,
	_ string,
	_ fosite.Requester,
) (err error) {
	return nil
}

func (s *store) GetAccessTokenSession(
	ctx context.Context,
	signature string,
	session fosite.Session,
) (fosite.Requester, error) {
	return &fosite.Request{Session: session}, nil
}

func (s *store) DeleteAccessTokenSession(_ context.Context, _ string) error {
	return nil
}

func (s *store) RevokeAccessToken(ctx context.Context, requestID string) error {
	return fmt.Errorf("access token revocation not supported")
}

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

	err := s.db.WithContext(ctx).Create(oauthSession).Error
	if err != nil {
		return err
	}

	return s.db.WithContext(ctx).
		Where(ConnectedApp{Subject: oauthSession.Subject, ClientID: oauthSession.ClientID}).
		Assign(ConnectedApp{Scopes: oauthSession.Scopes}).
		FirstOrCreate(&ConnectedApp{}).Error
}

func (s *store) DeleteRefreshTokenSession(ctx context.Context, signature string) error {
	return s.db.WithContext(ctx).Delete(&OAuthSession{}, "signature = ?", signature).Error
}

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
		return nil, err
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

func (s *store) RotateRefreshToken(
	ctx context.Context,
	requestID string,
	refreshTokenSignature string,
) (err error) {
	return s.DeleteRefreshTokenSession(ctx, refreshTokenSignature)
}

func (s *store) RevokeRefreshToken(_ context.Context, _ string) error {
	return fmt.Errorf("refresh token revocation not supported")
}
