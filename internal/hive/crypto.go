package hive

import "github.com/bluesky-social/indigo/atproto/atcrypto"

// generateSigningKeyPair generates a P-256 signing key pair and returns the
// public key and private key as multibase-encoded strings for storage.
// The private key must be encrypted before being persisted.
func generateSigningKeyPair() (pubMultibase string, privMultibase string, err error) {
	priv, err := atcrypto.GeneratePrivateKeyP256()
	if err != nil {
		return "", "", err
	}
	pub, err := priv.PublicKey()
	if err != nil {
		return "", "", err
	}
	return pub.Multibase(), priv.Multibase(), nil
}
