package oauthclient

import (
	"crypto"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/golang-jwt/jwt/v5"
)

// copied from indigo atproto/auth/oauth/jwt_signing.go so that we can sign own jwts with atcrypto private keys

var signingMethodES256 *signingMethodAtproto

type signingMethodAtproto struct {
	alg    string
	hash   crypto.Hash
	sigLen int
}

func init() {
	jwt.MarshalSingleStringAsArray = false

	signingMethodES256 = &signingMethodAtproto{
		alg:    "ES256",
		hash:   crypto.SHA256,
		sigLen: 64,
	}
	jwt.RegisterSigningMethod(signingMethodES256.Alg(), func() jwt.SigningMethod {
		return signingMethodES256
	})
}

func (sm *signingMethodAtproto) Verify(signingString string, sig []byte, key any) error {
	pub, ok := key.(atcrypto.PublicKey)
	if !ok {
		return jwt.ErrInvalidKeyType
	}

	if !sm.hash.Available() {
		return jwt.ErrHashUnavailable
	}

	if len(sig) != sm.sigLen {
		return jwt.ErrTokenSignatureInvalid
	}

	return pub.HashAndVerifyLenient([]byte(signingString), sig)
}

func (sm *signingMethodAtproto) Sign(signingString string, key any) ([]byte, error) {
	priv, ok := key.(atcrypto.PrivateKey)
	if !ok {
		return nil, jwt.ErrInvalidKeyType
	}

	return priv.HashAndSign([]byte(signingString))
}

func (sm *signingMethodAtproto) Alg() string {
	return sm.alg
}

func keySigningMethod(key atcrypto.PrivateKey) (jwt.SigningMethod, error) {
	switch key.(type) {
	case *atcrypto.PrivateKeyP256:
		return signingMethodES256, nil
	case *atcrypto.PrivateKeyK256:
		return nil, fmt.Errorf("only P-256 (ES256) private keys supported for atproto OAuth")
	}
	return nil, fmt.Errorf("unknown key type: %T", key)
}
