package utils

import (
	"time"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
)

func ServiceAuthToken(
	privateKey atcrypto.PrivateKey,
	iss syntax.DID,
	aud string,
	lxm *syntax.NSID,
	ttl *time.Duration,
) (string, error) {
	if ttl != nil {
		const maxTTL = 30 * time.Minute
		if *ttl > maxTTL {
			ttl = new(maxTTL)
		}
	} else {
		ttl = new(60 * time.Second)
	}
	return jwt.NewWithClaims(jwt.GetSigningMethod("ES256K"), jwt.MapClaims{
		"exp": jwt.NewNumericDate(time.Now().Add(*ttl)),
		"iat": jwt.NewNumericDate(time.Now()),
		"iss": iss.String(),
		"aud": aud,
		"jti": RandomNonce(16),
		"lxm": lxm,
	}).SignedString(privateKey)
}

// SpaceCredential mints a DPoP-bound space credential JWT for space,
// per the atproto permissioned-data proposal
// (https://github.com/bluesky-social/proposals/blob/main/0016-permissioned-data/README.md).
// jkt is the RFC 7638 JWK thumbprint of the key the credential is bound to
// (from the DPoP proof presented alongside the delegation token it was
// exchanged for); a caller presenting the credential must prove possession
// of that same key via a DPoP proof whose `jwk` header hashes to jkt.
func SpaceCredential(
	privateKey atcrypto.PrivateKey,
	kid string,
	space habitat_syntax.SpaceURI,
	jkt string,
) (string, error) {
	return new(jwt.Token{
		Method: jwt.GetSigningMethod("ES256K"),
		Claims: jwt.MapClaims{
			"iss": space.SpaceOwner(),
			"sub": space,
			"iat": jwt.NewNumericDate(time.Now()),
			"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
			"jti": RandomNonce(16),
			"cnf": map[string]string{"jkt": jkt},
		},
		Header: map[string]any{
			"typ": "atproto-space-credential+jwt",
			"kid": kid,
			"alg": "ES256K",
		},
	}).SignedString(privateKey)
}

func DelegationToken(
	privateKey atcrypto.PrivateKey,
	iss syntax.DID,
	kid string,
	space habitat_syntax.SpaceURI,
) (string, error) {
	return new(jwt.Token{
		Method: jwt.GetSigningMethod("ES256K"),
		Claims: jwt.MapClaims{
			"iss": iss,
			"sub": space.String(),
			"aud": space.SpaceOwner().String() + "#atproto_space",
			"iat": time.Now().Unix(),
			"exp": time.Now().Add(time.Minute).Unix(),
			"jti": RandomNonce(16),
		},
		Header: map[string]any{
			"typ": "atproto-space-delegation+jwt",
			"kid": kid,
			"alg": "ES256K",
		},
	}).SignedString(privateKey)
}
