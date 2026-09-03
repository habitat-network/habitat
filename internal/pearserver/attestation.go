package pearserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	jose "github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"

	"github.com/habitat-network/habitat/internal/clientmeta"
)

// attestationTyp is the required "typ" header on a client attestation JWT.
// See https://github.com/bluesky-social/proposals/blob/main/0016-permissioned-data/README.md#client-attestation.
const attestationTyp = "atproto-client-attestation+jwt"

// maxAttestationTTL bounds how long-lived an attestation's exp-iat window
// may be. The proposal's example attestations are ~60s; this leaves headroom
// for clock skew between the client and Habitat without accepting
// anomalously long-lived tokens.
const maxAttestationTTL = 5 * time.Minute

// attestationClockSkew is the allowance for clock skew between the client
// and Habitat when checking that an attestation's iat is not in the future.
// 60s matches the default service-auth token TTL used elsewhere in this
// codebase (see internal/utils/jwt.go) as a reasonable skew-tolerance
// convention.
const attestationClockSkew = 60 * time.Second

// ErrInvalidAttestation wraps every reason an attestation JWT is rejected:
// malformed, badly signed, or failing a claims check.
var ErrInvalidAttestation = errors.New("invalid client attestation")

// verifyAttestation verifies a client attestation JWT presented to
// getSpaceCredential and returns the verified client_id (the attestation's
// iss) on success.
func verifyAttestation(
	ctx context.Context,
	resolver *clientmeta.Resolver,
	raw string,
	spaceOwner syntax.DID,
) (string, error) {
	parsed, err := jwt.ParseSigned(raw)
	if err != nil {
		return "", fmt.Errorf("%w: parse: %v", ErrInvalidAttestation, err)
	}
	if len(parsed.Headers) != 1 {
		return "", fmt.Errorf("%w: expected exactly one signature", ErrInvalidAttestation)
	}
	header := parsed.Headers[0]
	if header.Algorithm != string(jose.ES256) {
		return "", fmt.Errorf("%w: unsupported alg %q", ErrInvalidAttestation, header.Algorithm)
	}
	typ, _ := header.ExtraHeaders[jose.HeaderType].(string)
	if typ != attestationTyp {
		return "", fmt.Errorf("%w: unexpected typ %q", ErrInvalidAttestation, typ)
	}
	if header.KeyID == "" {
		return "", fmt.Errorf("%w: missing kid", ErrInvalidAttestation)
	}

	// The iss claim names the client_id to resolve *before* the signature is
	// verified, which is inherent to this scheme (the verification key lives
	// at a location the token itself names). This is safe: an attacker who
	// doesn't hold the private key for a client_id's published JWKS cannot
	// produce a signature the next step accepts, regardless of what claims
	// they put in an unsigned-so-far token.
	var unverified jwt.Claims
	if err := parsed.UnsafeClaimsWithoutVerification(&unverified); err != nil {
		return "", fmt.Errorf("%w: read claims: %v", ErrInvalidAttestation, err)
	}
	if unverified.Issuer == "" {
		return "", fmt.Errorf("%w: missing iss", ErrInvalidAttestation)
	}

	key, err := resolver.ResolveKey(ctx, unverified.Issuer, header.KeyID)
	if errors.Is(err, clientmeta.ErrKeyNotFound) {
		return "", fmt.Errorf("%w: key not found: %v", ErrInvalidAttestation, err)
	} else if err != nil {
		// Don't propagate the raw fetch error to the caller: iss is
		// attacker-controlled, and distinguishing error text (connection
		// refused vs. DNS failure vs. non-200 vs. malformed JSON) turns this
		// into an SSRF oracle for arbitrary URLs. Log the real error
		// server-side and return a generic, client-facing one.
		slog.WarnContext(ctx, "failed to resolve client attestation key",
			"err", err, "iss", unverified.Issuer)
		return "", fmt.Errorf("%w: unable to resolve client key", ErrInvalidAttestation)
	}

	var claims jwt.Claims
	if err := parsed.Claims(key.Key, &claims); err != nil {
		return "", fmt.Errorf("%w: bad signature: %v", ErrInvalidAttestation, err)
	}

	if claims.Issuer == "" || claims.Issuer != claims.Subject {
		return "", fmt.Errorf("%w: iss must equal sub", ErrInvalidAttestation)
	}
	wantAud := spaceOwner.String() + "#atproto_space_host"
	if !claims.Audience.Contains(wantAud) {
		return "", fmt.Errorf("%w: aud does not match %q", ErrInvalidAttestation, wantAud)
	}
	if claims.ID == "" {
		return "", fmt.Errorf("%w: missing jti", ErrInvalidAttestation)
	}
	if claims.Expiry == nil || claims.IssuedAt == nil {
		return "", fmt.Errorf("%w: missing iat/exp", ErrInvalidAttestation)
	}
	now := time.Now()
	if claims.Expiry.Time().Before(now) {
		return "", fmt.Errorf("%w: expired", ErrInvalidAttestation)
	}
	if claims.Expiry.Time().Before(claims.IssuedAt.Time()) {
		return "", fmt.Errorf("%w: exp before iat", ErrInvalidAttestation)
	}
	if claims.Expiry.Time().Sub(claims.IssuedAt.Time()) > maxAttestationTTL {
		return "", fmt.Errorf("%w: exp too far from iat", ErrInvalidAttestation)
	}
	// The exp-iat delta check above doesn't bound either from wall-clock now:
	// an attestation with iat/exp both shifted far into the future (e.g.
	// iat=now+24h, exp=now+24h+1m) would otherwise pass every check so far.
	// Bound exp absolutely, and allow iat a small clock-skew allowance rather
	// than requiring it to be exactly <= now.
	if claims.IssuedAt.Time().After(now.Add(attestationClockSkew)) {
		return "", fmt.Errorf("%w: iat too far in the future", ErrInvalidAttestation)
	}
	if claims.Expiry.Time().After(now.Add(maxAttestationTTL)) {
		return "", fmt.Errorf("%w: exp too far in the future", ErrInvalidAttestation)
	}

	return claims.Issuer, nil
}
