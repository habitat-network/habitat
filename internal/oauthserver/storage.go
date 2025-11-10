package oauthserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/eagraf/habitat-new/internal/auth"
	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/handler/pkce"
	"github.com/ory/fosite/storage"
)

type store struct {
	memoryStore *storage.MemoryStore
	strategy    *strategy
}

func newStore(strat *strategy) *store {
	return &store{
		memoryStore: storage.NewMemoryStore(),
		strategy:    strat,
	}
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
	err = s.strategy.decrypt(code, &data)
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	client, err := s.GetClient(ctx, data.ClientID)
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	return &fosite.Request{
		Client:         client,
		Session:        &data,
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
	_ string,
	session fosite.Session,
) (fosite.Requester, error) {
	return &fosite.Request{
		Form: url.Values{
			"code_challenge":        []string{session.(*authSession).PKCEChallenge},
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
	var sess authSession
	err := s.strategy.decrypt(signature, &sess)
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	return &fosite.AccessRequest{
		Request: fosite.Request{
			Session: &sess,
		},
	}, nil
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
	panic("not implemented")
}

// DeleteRefreshTokenSession implements oauth2.CoreStorage.
func (s *store) DeleteRefreshTokenSession(ctx context.Context, signature string) error {
	panic("not implemented")
}

// GetRefreshTokenSession implements oauth2.CoreStorage.
func (s *store) GetRefreshTokenSession(
	ctx context.Context,
	signature string,
	session fosite.Session,
) (fosite.Requester, error) {
	panic("not implemented")
}

// RotateRefreshToken implements oauth2.CoreStorage.
func (s *store) RotateRefreshToken(
	ctx context.Context,
	requestID string,
	refreshTokenSignature string,
) (err error) {
	panic("not implemented")
}

// RevokeRefreshToken implements oauth2.TokenRevocationStorage.
func (s *store) RevokeRefreshToken(_ context.Context, _ string) error {
	return fmt.Errorf("refresh token revocation not supported")
}
