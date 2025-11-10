package oauthserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/fxamacker/cbor/v2"
	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/oauth2"
	"golang.org/x/crypto/nacl/secretbox"
)

type strategy struct {
	key *[32]byte
}

func newStrategy(key []byte) *strategy {
	var k [32]byte
	copy(k[:], key)
	return &strategy{key: &k}
}

var _ oauth2.CoreStrategy = &strategy{}

// AccessTokenSignature implements oauth2.CoreStrategy.
func (s *strategy) AccessTokenSignature(ctx context.Context, token string) string {
	return token
}

// GenerateAccessToken implements oauth2.CoreStrategy.
func (s *strategy) GenerateAccessToken(
	ctx context.Context,
	requester fosite.Requester,
) (token string, signature string, err error) {
	token, err = s.encrypt(newAccessTokenSession(requester.GetSession().(*authSession)))
	return token, token, err
}

// ValidateAccessToken implements oauth2.CoreStrategy.
func (s *strategy) ValidateAccessToken(
	ctx context.Context,
	requester fosite.Requester,
	token string,
) (err error) {
	return s.decrypt(token, nil)
}

// RefreshTokenSignature implements oauth2.CoreStrategy.
func (s *strategy) RefreshTokenSignature(ctx context.Context, token string) string {
	return token
}

// GenerateRefreshToken implements oauth2.CoreStrategy.
func (s *strategy) GenerateRefreshToken(
	ctx context.Context,
	requester fosite.Requester,
) (token string, signature string, err error) {
	token, err = s.encrypt(requester.GetSession().(*authSession))
	return token, token, err
}

// ValidateRefreshToken implements oauth2.CoreStrategy.
func (s *strategy) ValidateRefreshToken(
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
		secretbox.Seal(nonce[:], b.Bytes(), &nonce, s.key),
	), nil
}

func (s *strategy) decrypt(token string, data any) error {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}
	var nonce [24]byte
	copy(nonce[:], b[:24])
	decrypted, ok := secretbox.Open(nil, b[24:], &nonce, s.key)
	if !ok {
		return fmt.Errorf("invalid token")
	}
	if data != nil {
		return cbor.NewDecoder(bytes.NewReader(decrypted)).Decode(data)
	}
	return nil
}
