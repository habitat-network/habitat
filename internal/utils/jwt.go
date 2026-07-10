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
	ttl *time.Duration,
) (headers map[string]any, claims jwt.Claims) {
	if ttl != nil {
		const maxTTL = 30 * time.Minute
		if *ttl > maxTTL {
			ttl = new(maxTTL)
		}
	} else {
		ttl = new(60 * time.Second)
	}
	return map[string]any{}, jwt.MapClaims{
		"exp": jwt.NewNumericDate(time.Now().Add(*ttl)),
		"iat": jwt.NewNumericDate(time.Now()),
		"iss": iss.String(),
		"aud": aud,
		"jti": RandomNonce(16),
		"lxm": lxm,
	}
}
