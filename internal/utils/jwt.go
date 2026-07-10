package utils

import (
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
)

func ServiceAuthClaims(
	iss syntax.DID,
	aud string,
	lxm *syntax.NSID,
) (headers map[string]any, claims jwt.Claims) {
	return map[string]any{}, jwt.MapClaims{
		"exp": jwt.NewNumericDate(time.Now().Add(time.Minute)),
		"iat": jwt.NewNumericDate(time.Now()),
		"iss": iss,
		"aud": aud,
		"jti": RandomNonce(16),
		"lxm": lxm,
	}
}
