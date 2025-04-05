package bffauth

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/stretchr/testify/require"
)

func TestChallengeProofFlow(t *testing.T) {
	// Generate a test key pair
	privateKey, err := crypto.GeneratePrivateKeyP256()
	require.NoError(t, err, "failed to generate key pair")

	publicKey, err := privateKey.PublicKey()
	require.NoError(t, err, "failed to generate public key")

	// Generate a challenge
	challenge, err := GenerateChallenge()
	require.NoError(t, err, "failed to generate challenge")

	// Create proof with private key
	proof, err := GenerateProof(challenge, privateKey)
	require.NoError(t, err, "failed to generate proof")

	// Verify proof with public key
	valid, err := VerifyProof(challenge, proof, publicKey)
	require.NoError(t, err, "failed to verify proof")

	require.True(t, valid, "proof verification failed")
}
