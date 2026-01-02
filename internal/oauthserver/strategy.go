package oauthserver

import (
	"context"

	"github.com/eagraf/habitat-new/internal/encrypt"
	"github.com/go-jose/go-jose/v3"
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
func newStrategy(jwk *jose.JSONWebKey, config fosite.Configurator) *strategy {
	// Get global secret for authorization code encryption
	secret, err := config.GetGlobalSecret(context.Background())
	if err != nil {
		panic(err)
	}
	// Use the JWK for JWT signing
	jwtStrategy := compose.NewOAuth2JWTStrategy(func(ctx context.Context) (any, error) {
		return jwk, nil
	}, oauth2.NewHMACSHAStrategy(&hmac.HMACStrategy{
		Config: config,
	}, config), config)

	return &strategy{
		DefaultJWTStrategy: jwtStrategy,
		encryptionKey:      secret,
	}
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
