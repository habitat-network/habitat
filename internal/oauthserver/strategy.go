package oauthserver

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"

	"github.com/habitat-network/habitat/internal/encrypt"
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
) (token string, signature string, err error) {
	token, err = encrypt.EncryptCBOR(requester.GetSession().(*authSession), s.encryptionKey)
	return token, token, err
}

// ValidateAuthorizeCode implements oauth2.CoreStrategy.
func (s *strategy) ValidateAuthorizeCode(
	ctx context.Context,
	requester fosite.Requester,
	token string,
) (err error) {
	return encrypt.DecryptCBOR(token, s.encryptionKey, nil)
}

// AuthorizeCodeSignature implements oauth2.CoreStrategy.
func (s *strategy) AuthorizeCodeSignature(ctx context.Context, token string) string {
	return token
}
