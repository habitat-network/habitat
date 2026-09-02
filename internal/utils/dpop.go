package utils

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"time"

	jose "github.com/go-jose/go-jose/v3"
	josejwt "github.com/go-jose/go-jose/v3/jwt"
)

// DPoPProofClaims are the claims carried by a DPoP proof JWT. See RFC 9449
// §4.2 (https://datatracker.ietf.org/doc/html/rfc9449#section-4.2) and the
// atproto permissioned-data proposal
// (https://github.com/bluesky-social/proposals/blob/main/0016-permissioned-data/README.md),
// which reuses DPoP to bind a space credential to a client-held key.
type DPoPProofClaims struct {
	josejwt.Claims

	// the `htm` (HTTP Method) claim.
	Method string `json:"htm"`

	// the `htu` (HTTP URL) claim, without query or fragment components.
	URL string `json:"htu"`

	// the `ath` (Authorization Token Hash) claim: present only when the
	// proof accompanies a bound token (e.g. a space credential presented
	// as an access token), not when it merely proves possession of a key
	// that a token is about to be minted against.
	AccessTokenHash string `json:"ath,omitempty"`
}

// GenerateDPoPKey creates a new ES256 keypair to bind a DPoP proof to.
func GenerateDPoPKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// DPoPProofURL strips the query and fragment from rawURL, as required for
// the `htu` claim of a DPoP proof (RFC 9449 §4.2).
func DPoPProofURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// SignDPoPProof builds and signs a DPoP proof JWT over method and rawURL,
// proving possession of key. accessToken, when non-empty, is bound into the
// proof's `ath` claim so the proof can only be replayed alongside that
// specific token.
func SignDPoPProof(
	key *ecdsa.PrivateKey,
	method string,
	rawURL string,
	accessToken string,
) (string, error) {
	htu, err := DPoPProofURL(rawURL)
	if err != nil {
		return "", err
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: key},
		&jose.SignerOptions{
			ExtraHeaders: map[jose.HeaderKey]any{
				jose.HeaderType: "dpop+jwt",
				"jwk": &jose.JSONWebKey{
					Key:       key.Public(),
					Use:       "sig",
					Algorithm: string(jose.ES256),
				},
			},
		},
	)
	if err != nil {
		return "", fmt.Errorf("build dpop signer: %w", err)
	}
	claims := DPoPProofClaims{
		Claims: josejwt.Claims{
			ID:       RandomNonce(16),
			IssuedAt: josejwt.NewNumericDate(time.Now()),
		},
		Method: method,
		URL:    htu,
	}
	if accessToken != "" {
		claims.AccessTokenHash = HashDPoPToken(accessToken)
	}
	return josejwt.Signed(signer).Claims(claims).CompactSerialize()
}

// HashDPoPToken computes the `ath` claim value for token: the base64url
// (no padding) encoded SHA-256 hash of the token's ASCII encoding (RFC 9449
// §4.2).
func HashDPoPToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}
