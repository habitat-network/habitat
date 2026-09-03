package clientmetadata

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"fmt"
	"math/big"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	jose "github.com/go-jose/go-jose/v3"
)

// ConvertJWK converts an atproto JWK (an EC public key, as used in client
// metadata documents) into a go-jose JSONWebKey usable for signature
// verification. Only the curves go-jose understands are supported; ES256
// (and this package's callers, which all verify ES256-signed JWTs) never use
// secp256k1, so it is rejected.
func ConvertJWK(jwk atcrypto.JWK) (*jose.JSONWebKey, error) {
	var curve elliptic.Curve
	switch jwk.Curve {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("unsupported JWK curve %q", jwk.Curve)
	}
	if jwk.KeyType != "EC" {
		return nil, fmt.Errorf("unsupported JWK key type %q", jwk.KeyType)
	}
	x, err := base64.RawURLEncoding.DecodeString(jwk.X)
	if err != nil {
		return nil, fmt.Errorf("invalid JWK x coordinate: %w", err)
	}
	y, err := base64.RawURLEncoding.DecodeString(jwk.Y)
	if err != nil {
		return nil, fmt.Errorf("invalid JWK y coordinate: %w", err)
	}
	var keyID string
	if jwk.KeyID != nil {
		keyID = *jwk.KeyID
	}
	return &jose.JSONWebKey{
		//nolint:staticcheck // SA1019: deprecated ecdsa.PublicKey X/Y fields
		Key: &ecdsa.PublicKey{
			Curve: curve,
			X:     new(big.Int).SetBytes(x),
			Y:     new(big.Int).SetBytes(y),
		},
		KeyID: keyID,
	}, nil
}
