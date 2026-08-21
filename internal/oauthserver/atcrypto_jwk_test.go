package oauthserver

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/stretchr/testify/require"
)

func TestAtcryptoJWKtoJose(t *testing.T) {
	t.Run("converts a P-256 public key", func(t *testing.T) {
		priv, err := atcrypto.GeneratePrivateKeyP256()
		require.NoError(t, err)
		pub, err := priv.PublicKey()
		require.NoError(t, err)
		jwk, err := pub.JWK()
		require.NoError(t, err)
		kid := "test-key"
		jwk.KeyID = &kid

		got, err := atcryptoJWKtoJose(*jwk)
		require.NoError(t, err)
		require.Equal(t, kid, got.KeyID)

		ecPub, ok := got.Key.(*ecdsa.PublicKey)
		require.True(t, ok, "expected *ecdsa.PublicKey, got %T", got.Key)
		require.Equal(t, elliptic.P256(), ecPub.Curve)

		xb, err := base64.RawURLEncoding.DecodeString(jwk.X)
		require.NoError(t, err)
		yb, err := base64.RawURLEncoding.DecodeString(jwk.Y)
		require.NoError(t, err)
		// SEC 1 uncompressed point: 0x04 || X || Y.
		want := append([]byte{4}, append(xb, yb...)...)
		gotBytes, err := ecPub.Bytes()
		require.NoError(t, err)
		require.Equal(t, want, gotBytes)
	})

	t.Run("returns an empty key id when the jwk has none", func(t *testing.T) {
		priv, err := atcrypto.GeneratePrivateKeyP256()
		require.NoError(t, err)
		pub, err := priv.PublicKey()
		require.NoError(t, err)
		jwk, err := pub.JWK()
		require.NoError(t, err)

		got, err := atcryptoJWKtoJose(*jwk)
		require.NoError(t, err)
		require.Empty(t, got.KeyID)
	})

	t.Run("rejects unsupported curves", func(t *testing.T) {
		jwk := atcrypto.JWK{
			KeyType: "EC",
			Curve:   "secp256k1",
			X:       base64.RawURLEncoding.EncodeToString([]byte{1}),
			Y:       base64.RawURLEncoding.EncodeToString([]byte{2}),
		}
		_, err := atcryptoJWKtoJose(jwk)
		require.ErrorContains(t, err, "unsupported")
	})

	t.Run("rejects non-EC key types", func(t *testing.T) {
		jwk := atcrypto.JWK{
			KeyType: "RSA",
		}
		_, err := atcryptoJWKtoJose(jwk)
		require.ErrorContains(t, err, "unsupported")
	})

	t.Run("rejects invalid base64 coordinates", func(t *testing.T) {
		jwk := atcrypto.JWK{
			KeyType: "EC",
			Curve:   "P-256",
			X:       "!!!not-base64!!!",
			Y:       "dGVzdA",
		}
		_, err := atcryptoJWKtoJose(jwk)
		require.Error(t, err)
	})
}
