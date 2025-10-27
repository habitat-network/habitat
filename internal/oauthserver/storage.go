package oauthserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/eagraf/habitat-new/internal/auth"
	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/handler/pkce"
	"github.com/ory/fosite/storage"
)

type store struct {
	memoryStore *storage.MemoryStore
}

func newStore() *store {
	return &store{memoryStore: storage.NewMemoryStore()}
}

var (
	_ fosite.Storage                = (*store)(nil)
	_ oauth2.CoreStorage            = (*store)(nil)
	_ oauth2.TokenRevocationStorage = (*store)(nil)
	_ pkce.PKCERequestStorage       = (*store)(nil)
)

// ClientAssertionJWTValid implements fosite.Storage.
func (s *store) ClientAssertionJWTValid(ctx context.Context, jti string) error {
	return s.memoryStore.ClientAssertionJWTValid(ctx, jti)
}

// GetClient implements fosite.Storage.
func (s *store) GetClient(ctx context.Context, id string) (fosite.Client, error) {
	// TODO: consider caching
	resp, err := http.Get(id)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch client metadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var metadata auth.ClientMetadata
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

// CreateAccessTokenSession implements oauth2.CoreStorage.
func (s *store) CreateAccessTokenSession(
	ctx context.Context,
	signature string,
	request fosite.Requester,
) (err error) {
	return s.memoryStore.CreateAccessTokenSession(ctx, signature, request)
}

// CreateAuthorizeCodeSession implements oauth2.CoreStorage.
func (s *store) CreateAuthorizeCodeSession(
	ctx context.Context,
	code string,
	request fosite.Requester,
) (err error) {
	return s.memoryStore.CreateAuthorizeCodeSession(ctx, code, request)
}

// CreateRefreshTokenSession implements oauth2.CoreStorage.
func (s *store) CreateRefreshTokenSession(
	ctx context.Context,
	signature string,
	accessSignature string,
	request fosite.Requester,
) (err error) {
	return s.memoryStore.CreateRefreshTokenSession(ctx, signature, accessSignature, request)
}

// DeleteAccessTokenSession implements oauth2.CoreStorage.
func (s *store) DeleteAccessTokenSession(ctx context.Context, signature string) (err error) {
	return s.memoryStore.DeleteAccessTokenSession(ctx, signature)
}

// DeleteRefreshTokenSession implements oauth2.CoreStorage.
func (s *store) DeleteRefreshTokenSession(ctx context.Context, signature string) (err error) {
	return s.memoryStore.DeleteRefreshTokenSession(ctx, signature)
}

// GetAccessTokenSession implements oauth2.CoreStorage.
func (s *store) GetAccessTokenSession(
	ctx context.Context,
	signature string,
	session fosite.Session,
) (request fosite.Requester, err error) {
	return s.memoryStore.GetAccessTokenSession(ctx, signature, session)
}

// GetAuthorizeCodeSession implements oauth2.CoreStorage.
func (s *store) GetAuthorizeCodeSession(
	ctx context.Context,
	code string,
	session fosite.Session,
) (request fosite.Requester, err error) {
	return s.memoryStore.GetAuthorizeCodeSession(ctx, code, session)
}

// GetRefreshTokenSession implements oauth2.CoreStorage.
func (s *store) GetRefreshTokenSession(
	ctx context.Context,
	signature string,
	session fosite.Session,
) (request fosite.Requester, err error) {
	return s.memoryStore.GetRefreshTokenSession(ctx, signature, session)
}

// InvalidateAuthorizeCodeSession implements oauth2.CoreStorage.
func (s *store) InvalidateAuthorizeCodeSession(ctx context.Context, code string) (err error) {
	return s.memoryStore.InvalidateAuthorizeCodeSession(ctx, code)
}

// RotateRefreshToken implements oauth2.CoreStorage.
func (s *store) RotateRefreshToken(
	ctx context.Context,
	requestID string,
	refreshTokenSignature string,
) (err error) {
	return s.memoryStore.RevokeRefreshToken(ctx, requestID)
}

// RevokeAccessToken implements oauth2.TokenRevocationStorage.
func (s *store) RevokeAccessToken(ctx context.Context, requestID string) error {
	return s.memoryStore.RevokeAccessToken(ctx, requestID)
}

// RevokeRefreshToken implements oauth2.TokenRevocationStorage.
func (s *store) RevokeRefreshToken(ctx context.Context, requestID string) error {
	return s.memoryStore.RevokeRefreshToken(ctx, requestID)
}

// CreatePKCERequestSession implements pkce.PKCERequestStorage.
func (s *store) CreatePKCERequestSession(
	ctx context.Context,
	signature string,
	requester fosite.Requester,
) error {
	return s.memoryStore.CreatePKCERequestSession(ctx, signature, requester)
}

// DeletePKCERequestSession implements pkce.PKCERequestStorage.
func (s *store) DeletePKCERequestSession(ctx context.Context, signature string) error {
	return s.memoryStore.DeletePKCERequestSession(ctx, signature)
}

// GetPKCERequestSession implements pkce.PKCERequestStorage.
func (s *store) GetPKCERequestSession(
	ctx context.Context,
	signature string,
	session fosite.Session,
) (fosite.Requester, error) {
	return s.memoryStore.GetPKCERequestSession(ctx, signature, session)
}
