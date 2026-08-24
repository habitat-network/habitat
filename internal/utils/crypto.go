package utils

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/x509"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
)

func P256PrivateKeyToECDSA(input *atcrypto.PrivateKeyP256) (*ecdsa.PrivateKey, error) {
	skECDH, err := ecdh.P256().NewPrivateKey(input.Bytes())
	if err != nil {
		return nil, fmt.Errorf("invalid P-256/secp256r1 private key: %w", err)
	}
	enc, err := x509.MarshalPKCS8PrivateKey(skECDH)
	if err != nil {
		return nil, fmt.Errorf("invalid P-256/secp256r1 private key: %w", err)
	}
	sk, err := x509.ParsePKCS8PrivateKey(enc)
	if err != nil {
		return nil, fmt.Errorf("invalid P-256/secp256r1 private key: %w", err)
	}
	skECDSA, ok := sk.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf(
			"unexpected internal error parsing own private P-256 x509 key: %w",
			err,
		)
	}
	return skECDSA, nil
}
