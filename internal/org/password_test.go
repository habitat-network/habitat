package org

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHashPassword_VerifySamePassword(t *testing.T) {
	hash, err := hashPassword("correct-horse-battery-staple")
	require.NoError(t, err)
	require.NotEmpty(t, hash)

	ok, err := verifyPassword("correct-horse-battery-staple", hash)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestVerifyPassword_WrongPassword(t *testing.T) {
	hash, err := hashPassword("correct-horse-battery-staple")
	require.NoError(t, err)

	ok, err := verifyPassword("wrong-password", hash)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestHashPassword_UniqueHashes(t *testing.T) {
	// Two hashes of the same password must differ (unique salts)
	hash1, err := hashPassword("same-password")
	require.NoError(t, err)
	hash2, err := hashPassword("same-password")
	require.NoError(t, err)
	require.NotEqual(t, hash1, hash2)
}
