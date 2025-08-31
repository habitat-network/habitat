package auth

import (
	"encoding/base64"

	"github.com/google/uuid"
)

func generateNonce() string {
	return base64.StdEncoding.EncodeToString([]byte(uuid.NewString()))
}
