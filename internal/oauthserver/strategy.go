package oauthserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-jose/go-jose/v3"
	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
	"github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/token/hmac"
	"golang.org/x/crypto/nacl/secretbox"
)

// strategy uses custom encryption for authorization codes (stateless)
// and JWT for access tokens (with database storage)
type strategy struct {
	*oauth2.DefaultJWTStrategy
	encryptionKey *[32]byte
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

	// Setup encryption key for authorization codes
	var encKey [32]byte
	copy(encKey[:], secret)

	return &strategy{
		DefaultJWTStrategy: jwtStrategy,
		encryptionKey:      &encKey,
	}
}

// GenerateAuthorizeCode implements oauth2.CoreStrategy.
func (s *strategy) GenerateAuthorizeCode(
	ctx context.Context,
	requester fosite.Requester,
) (token string, signature string, err error) {
	token, err = s.encrypt(requester.GetSession().(*authSession))
	return token, token, err
}

// ValidateAuthorizeCode implements oauth2.CoreStrategy.
func (s *strategy) ValidateAuthorizeCode(
	ctx context.Context,
	requester fosite.Requester,
	token string,
) (err error) {
	return s.decrypt(token, nil)
}

// AuthorizeCodeSignature implements oauth2.CoreStrategy.
func (s *strategy) AuthorizeCodeSignature(ctx context.Context, token string) string {
	return token
}

// Helper methods for encryption/decryption
func (s *strategy) encrypt(data any) (string, error) {
	var b bytes.Buffer
	if err := cbor.NewEncoder(&b).Encode(data); err != nil {
		return "", fmt.Errorf("failed to encode data: %w", err)
	}

	var nonce [24]byte
	_, err := io.ReadFull(rand.Reader, nonce[:])
	if err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(
		secretbox.Seal(nonce[:], b.Bytes(), &nonce, s.encryptionKey),
	), nil
}

func (s *strategy) decrypt(token string, data any) error {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}
	var nonce [24]byte
	copy(nonce[:], b[:24])
	decrypted, ok := secretbox.Open(nil, b[24:], &nonce, s.encryptionKey)
	if !ok {
		return fmt.Errorf("invalid token")
	}
	if data != nil {
		return cbor.NewDecoder(bytes.NewReader(decrypted)).Decode(data)
	}
	return nil
}
