package utils

import (
	"crypto/rand"
	"encoding/base64"
)

func RandomNonce(size int) string {
	buf := make([]byte, size)
	rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}
