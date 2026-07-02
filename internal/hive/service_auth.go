package hive

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	// Registers the atproto ES256/ES256K JWT signing methods (and configures
	// single-value `aud` serialization) used by signServiceAuth.
	_ "github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
)

// serviceAuthClaims mirrors the atproto service-auth JWT claim set (see indigo's
// auth.SignServiceAuth) plus a habitat-specific "org" claim naming the
// organization the issuer acted as. Downstream habitat services (e.g. the docs
// server) read it to act on the org's behalf.
type serviceAuthClaims struct {
	jwt.RegisteredClaims
	LexMethod string `json:"lxm,omitempty"`
	Org       string `json:"org,omitempty"`
}

// signServiceAuth signs an atproto-compatible service-auth JWT with the member's
// key, adding the org claim. indigo's auth.SignServiceAuth can't carry custom
// claims, so this replicates it; the ES256/ES256K signing methods it relies on
// are registered by indigo's auth package init.
func signServiceAuth(
	iss syntax.DID,
	aud string,
	org syntax.DID,
	ttl time.Duration,
	lxm *syntax.NSID,
	priv atcrypto.PrivateKey,
) (string, error) {
	claims := serviceAuthClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    iss.String(),
			Audience:  jwt.ClaimStrings{aud},
			ID:        randomNonce(),
		},
	}
	if lxm != nil {
		claims.LexMethod = lxm.String()
	}
	if org != "" {
		claims.Org = org.String()
	}

	alg, err := jwtAlgForKey(priv)
	if err != nil {
		return "", err
	}
	method := jwt.GetSigningMethod(alg)
	if method == nil {
		return "", fmt.Errorf("signing method %q not registered", alg)
	}
	return jwt.NewWithClaims(method, claims).SignedString(priv)
}

// jwtAlgForKey maps an atproto private key to its JWT `alg`. atcrypto.PrivateKey
// exposes no algorithm accessor, so the concrete key type is inspected (the same
// approach indigo takes).
func jwtAlgForKey(priv atcrypto.PrivateKey) (string, error) {
	switch priv.(type) {
	case *atcrypto.PrivateKeyP256:
		return "ES256", nil
	case *atcrypto.PrivateKeyK256:
		return "ES256K", nil
	default:
		return "", fmt.Errorf("unknown signing key type: %T", priv)
	}
}

func randomNonce() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}
