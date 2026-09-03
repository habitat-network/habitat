package spaces

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"

	_ "github.com/bluesky-social/indigo/atproto/auth" // registers the ES256/ES256K signing methods jwt.GetSigningMethod resolves below
	"github.com/habitat-network/habitat/internal/clientmeta"
)

// AttestationTyp is the required "typ" header on a client attestation JWT.
// See https://github.com/bluesky-social/proposals/blob/main/0016-permissioned-data/README.md#client-attestation.
const AttestationTyp = "atproto-client-attestation+jwt"

// maxAttestationTTL bounds how long-lived an attestation's exp-iat window
// may be. The proposal's example attestations are ~60s; this leaves headroom
// for clock skew between the client and Habitat without accepting
// anomalously long-lived tokens. Unlike iat/exp-vs-now (enforced by
// jwt.WithIssuedAt/WithExpirationRequired below, with attestationLeeway),
// this catches a legitimately-fresh iat paired with an implausibly distant
// exp.
const maxAttestationTTL = 5 * time.Minute

// attestationLeeway is the clock-skew allowance jwt/v5 applies to iat/exp
// comparisons against wall-clock now. In particular, WithIssuedAt rejects an
// iat more than this far in the future — which is what actually stops an
// attacker from minting an attestation dated far ahead of now, not
// maxAttestationTTL (a relative bound doesn't catch iat and exp shifted
// together). 60s matches the default service-auth token TTL used elsewhere
// in this codebase (see internal/utils/jwt.go) as a reasonable
// skew-tolerance convention.
const attestationLeeway = 60 * time.Second

// ErrInvalidAttestation wraps every reason an attestation JWT is rejected:
// malformed, badly signed, or failing a claims check.
var ErrInvalidAttestation = errors.New("invalid client attestation")

type attestationClaims struct {
	jwt.RegisteredClaims
}

// VerifyAttestation verifies a client attestation JWT presented to
// getSpaceCredential and returns the verified client_id (the attestation's
// iss) on success.
func VerifyAttestation(
	ctx context.Context,
	resolver *clientmeta.Resolver,
	raw string,
	spaceOwner syntax.DID,
) (string, error) {
	wantAud := spaceOwner.String() + "#atproto_space_host"

	claims := &attestationClaims{}
	token, err := jwt.ParseWithClaims(
		raw,
		claims,
		attestationKeyFunc(ctx, resolver),
		jwt.WithValidMethods([]string{"ES256"}),
		jwt.WithAudience(wantAud),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(attestationLeeway),
	)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidAttestation, err)
	}
	if token.Header["typ"] != AttestationTyp {
		return "", fmt.Errorf("%w: unexpected typ %v", ErrInvalidAttestation, token.Header["typ"])
	}
	if claims.Issuer == "" || claims.Issuer != claims.Subject {
		return "", fmt.Errorf("%w: iss must equal sub", ErrInvalidAttestation)
	}
	if claims.ID == "" {
		return "", fmt.Errorf("%w: missing jti", ErrInvalidAttestation)
	}
	if claims.ExpiresAt == nil || claims.IssuedAt == nil {
		return "", fmt.Errorf("%w: missing iat/exp", ErrInvalidAttestation)
	}
	if claims.ExpiresAt.Sub(claims.IssuedAt.Time) > maxAttestationTTL {
		return "", fmt.Errorf("%w: exp too far from iat", ErrInvalidAttestation)
	}

	return claims.Issuer, nil
}

// attestationKeyFunc resolves the ES256 verification key published by the
// attestation's (unverified, at this point) iss/kid, matching the
// DID-directory keyfunc pattern in internal/authn/util.go's
// fetchIssuerKeyFunc — except the key source here is a client's published
// JWKS (internal/clientmeta) rather than an atproto identity directory.
func attestationKeyFunc(ctx context.Context, resolver *clientmeta.Resolver) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		claims, ok := token.Claims.(*attestationClaims)
		if !ok {
			return nil, jwt.ErrTokenInvalidClaims
		}
		iss, err := claims.GetIssuer()
		if err != nil || iss == "" {
			return nil, fmt.Errorf("missing iss: %w", err)
		}
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, fmt.Errorf("missing kid")
		}

		key, err := resolver.ResolveAtprotoKey(ctx, iss, kid)
		if errors.Is(err, clientmeta.ErrKeyNotFound) {
			return nil, err
		} else if err != nil {
			// Don't propagate the raw fetch error: iss is attacker-controlled,
			// and distinguishing error text (connection refused vs. DNS
			// failure vs. non-200 vs. malformed JSON) turns this into an SSRF
			// oracle for arbitrary URLs. Log the real error server-side and
			// return a generic one that VerifyAttestation's caller sees.
			slog.WarnContext(ctx, "failed to resolve client attestation key",
				"err", err, "iss", iss)
			return nil, errors.New("unable to resolve client key")
		}
		return key, nil
	}
}
