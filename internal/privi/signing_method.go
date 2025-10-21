// Copied from https://github.com/ureeves/jwt-go-secp256k1/blob/v0.2.0/secp256k1.go
//
// Package secp256k1 implements a jwt.SigningMethod for secp256k1 signatures.
//
// Two different algorithms are implemented: ES256K and ES256K-R. The former
// produces and verifies using signatures in the R || S format, and the latter
// in R || S || V. V is the recovery byte, making it possible to recover public
// keys from signatures.
package privi

import (
	"crypto"
	"errors"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
)

// SigningMethodSecp256k1 is the implementation of jwt.SigningMethod.
type SigningMethodSecp256k1 struct {
	alg      string
	hash     crypto.Hash
	toOutSig toOutSig
	sigLen   int
}

// encodes a produced signature to the correct output - either in R || S or
// R || S || V format.
type toOutSig func(sig []byte) []byte

// Errors returned on different problems.
var (
	ErrWrongKeyFormat  = errors.New("wrong key type")
	ErrBadSignature    = errors.New("bad signature")
	ErrVerification    = errors.New("signature verification failed")
	ErrFailedSigning   = errors.New("failed generating signature")
	ErrHashUnavailable = errors.New("hasher unavailable")
)

// Verify verifies a secp256k1 signature in a JWT. The type of key has to be
// *ecdsa.PublicKey.
//
// Verify it is a secp256k1 key before passing, otherwise it will validate with
// that type of key instead. This can be done using ethereum's crypto package.
func (sm *SigningMethodSecp256k1) Verify(signingString string, signature []byte, key interface{}) error {
	pub, ok := key.(*atcrypto.PublicKeyK256)
	if !ok {
		return ErrWrongKeyFormat
	}

	if !sm.hash.Available() {
		return ErrHashUnavailable
	}

	return pub.HashAndVerify([]byte(signingString), signature)
}

// Sign produces a secp256k1 signature for a JWT. The type of key has
// to be *PrivateKey.
func (sm *SigningMethodSecp256k1) Sign(signingString string, key interface{}) ([]byte, error) {
	// Don't need this
	return nil, nil
}

// Alg returns the algorithm name.
func (sm *SigningMethodSecp256k1) Alg() string {
	return sm.alg
}

func toES256K(sig []byte) []byte {
	return sig[:64]
}
