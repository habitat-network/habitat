package org

import (
	// Use this argon2id wrapper rather than rolling our own auth
	"errors"

	"github.com/alexedwards/argon2id"
)

// HashPassword hashes a plaintext password using argon2id and returns a
// self-describing encoded string that includes the parameters and salt.
func hashPassword(password string) (string, error) {
	return argon2id.CreateHash(password, argon2id.DefaultParams)
}

// VerifyPassword checks a plaintext password against an encoded argon2id hash.
// Returns false, nil on mismatch; error only if the hash is malformed.
func verifyPassword(password, encodedHash string) (bool, error) {
	ok, err := argon2id.ComparePasswordAndHash(password, encodedHash)
	if errors.Is(err, argon2id.ErrInvalidHash) {
		return false, nil
	}
	return ok, err
}
