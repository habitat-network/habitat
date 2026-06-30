package oauthserver

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"

	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
	"github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/token/hmac"
)

// strategy uses custom encryption for authorization codes (stateless)
// and JWT for access tokens (with database storage)
type strategy struct {
	*oauth2.DefaultJWTStrategy
	encryptionKey []byte
}

var _ oauth2.CoreStrategy = &strategy{}

// newStrategy creates a hybrid strategy
func newStrategy(secret []byte, config fosite.Configurator) (*strategy, error) {
	privateKey, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), secret)
	if err != nil {
		return nil, err
	}
	// Use the JWK for JWT signing
	jwtStrategy := compose.NewOAuth2JWTStrategy(func(ctx context.Context) (any, error) {
		return privateKey, nil
	}, oauth2.NewHMACSHAStrategy(&hmac.HMACStrategy{
		Config: config,
	}, config), config)

	return &strategy{
		DefaultJWTStrategy: jwtStrategy,
		encryptionKey:      secret,
	}, nil
}

// GenerateAuthorizeCode implements oauth2.CoreStrategy.
func (s *strategy) GenerateAuthorizeCode(
	ctx context.Context,
	requester fosite.Requester,
) (code string, signature string, err error) {
	code, err = requester.GetSession().(*session).getAuthCode(s.encryptionKey)
	return code, code, err
}

// ValidateAuthorizeCode implements oauth2.CoreStrategy.
func (s *strategy) ValidateAuthorizeCode(
	ctx context.Context,
	requester fosite.Requester,
	code string,
) error {
	_, err := decodeSession(code, s.encryptionKey)
	if err != nil {
		return err
	}
	return nil
}

// AuthorizeCodeSignature implements oauth2.CoreStrategy.
func (s *strategy) AuthorizeCodeSignature(ctx context.Context, code string) string {
	return code
}
