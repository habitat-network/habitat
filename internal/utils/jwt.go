package utils

import (
	"time"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	// Registers the ES256K and ES256 signing methods for atcrypto keys.
	_ "github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
)

// signingMethod is the JWT algorithm for key: ES256 for a P-256 key, ES256K
// for a K-256 one. (indigo's signing methods sign with whatever key they are
// given, so the header is all that differs, but verifiers such as the atproto
// PDS check it against the issuer's published key.)
func signingMethod(key atcrypto.PrivateKey) jwt.SigningMethod {
	if _, ok := key.(*atcrypto.PrivateKeyP256); ok {
		return jwt.GetSigningMethod("ES256")
	}
	return jwt.GetSigningMethod("ES256K")
}

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
	return jwt.NewWithClaims(signingMethod(privateKey), jwt.MapClaims{
		"exp": jwt.NewNumericDate(time.Now().Add(*ttl)),
		"iat": jwt.NewNumericDate(time.Now()),
		"iss": iss.String(),
		"aud": aud,
		"jti": RandomNonce(16),
		"lxm": lxm,
	}).SignedString(privateKey)
}

// SpaceCredentialOpt customizes a space credential.
type SpaceCredentialOpt func(claims jwt.MapClaims)

// WithConfirmationKey binds the credential to a P-256 did:key via cnf.kid
// (RFC 7800): it is only usable with a request signature by that key.
func WithConfirmationKey(didKey string) SpaceCredentialOpt {
	return func(claims jwt.MapClaims) {
		claims["cnf"] = map[string]any{"kid": didKey}
	}
}

const (
	// DefaultSpaceCredentialTTL is the lifetime of a minted space credential.
	// Short expiry is the primary revocation mechanism for space credentials.
	DefaultSpaceCredentialTTL = 10 * time.Minute
	// MaxSpaceCredentialTTL is the longest a space credential may live.
	MaxSpaceCredentialTTL = 60 * time.Minute
)

// SpaceCredential mints a space credential issued by the space owner. Per the
// spaces alpha, a space authority resolves like any other account, so the
// credential is always signed with the owner's #atproto key.
func SpaceCredential(
	privateKey atcrypto.PrivateKey,
	space habitat_syntax.SpaceURI,
	opts ...SpaceCredentialOpt,
) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": space.SpaceOwner(),
		"sub": space.AtSpace(),
		"iat": jwt.NewNumericDate(now),
		"exp": jwt.NewNumericDate(now.Add(DefaultSpaceCredentialTTL)),
		// Random unique identifier, used for revocation.
		"jti": RandomNonce(16),
	}
	for _, opt := range opts {
		opt(claims)
	}
	return new(jwt.Token{
		Method: signingMethod(privateKey),
		Claims: claims,
		Header: map[string]any{
			"typ": "atproto-space-credential+jwt",
			"kid": "#atproto",
			"alg": signingMethod(privateKey).Alg(),
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
		Method: signingMethod(privateKey),
		Claims: jwt.MapClaims{
			"iss": iss,
			"sub": space.AtSpace().String(),
			"aud": space.SpaceOwner().String() + "#atproto_pds",
			"iat": time.Now().Unix(),
			"exp": time.Now().Add(time.Minute).Unix(),
			"jti": RandomNonce(16),
		},
		Header: map[string]any{
			"typ": "atproto-space-delegation+jwt",
			"kid": kid,
			"alg": signingMethod(privateKey).Alg(),
		},
	}).SignedString(privateKey)
}
