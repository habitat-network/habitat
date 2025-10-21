package bffauth

import (
	"encoding/base64"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/google/uuid"
)

// GenerateChallenge creates a random challenge string for client authentication.
func GenerateChallenge() (string, error) {
	return uuid.New().String(), nil
}

// GenerateProof creates a proof of possession using a private key.
// It signs the challenge with the provided private key to prove ownership.
func GenerateProof(challenge string, privateKey atcrypto.PrivateKey) (string, error) {
	// Sign the challenge bytes with the private key using ECDSA in ASN.1 format
	sig, err := privateKey.HashAndSign([]byte(challenge))
	if err != nil {
		return "", err
	}

	// Encode signature as base64 string
	proof := base64.StdEncoding.EncodeToString(sig)
	return proof, nil
}

// VerifyProof checks if a proof is valid for a given challenge and public key.
// It verifies that the proof was generated using the corresponding private key.
func VerifyProof(challenge string, proof string, publicKey atcrypto.PublicKey) (bool, error) {
	// Decode the proof signature from base64
	signatureBytes, err := base64.StdEncoding.DecodeString(proof)
	if err != nil {
		return false, err
	}

	// Verify the ECDSA signature
	err = publicKey.HashAndVerify([]byte(challenge), signatureBytes)
	if err != nil {
		return false, err
	}

	return true, nil
}
