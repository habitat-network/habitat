package authn

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/habitat-network/habitat/internal/httpsig"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/keyval"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"

	"github.com/golang-jwt/jwt/v5"
)

// spaceCredentialKid is the only key ID a space credential may be signed with.
const spaceCredentialKid = "#atproto"

// CredentialRevocationTTL is how long a repo host remembers a revoked space
// credential's jti. Credentials live at most 60 minutes, so after this no
// credential with that jti can still be valid.
const CredentialRevocationTTL = 60 * time.Minute

// CredentialRevocations records the space credentials a space authority has
// revoked through notifyCredentialRevoked, by (space, jti), so repo reads
// presenting them are refused until they would have expired anyway.
type CredentialRevocations struct {
	kv keyval.KV
}

// NewCredentialRevocations stores revocations in kv, which should be a
// namespace of its own.
func NewCredentialRevocations(kv keyval.KV) *CredentialRevocations {
	return &CredentialRevocations{kv: kv}
}

func revocationKey(space habitat_syntax.SpaceURI, jti string) string {
	return string(space) + " " + jti
}

// Revoke marks each jti revoked for space. It is idempotent: revoking a jti
// again only extends how long it is remembered.
func (c *CredentialRevocations) Revoke(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	jtis []string,
) error {
	for _, jti := range jtis {
		if err := c.kv.Put(ctx, revocationKey(space, jti), true, CredentialRevocationTTL); err != nil {
			return fmt.Errorf("revoke %s: %w", jti, err)
		}
	}
	return nil
}

// IsRevoked reports whether jti was revoked for space.
func (c *CredentialRevocations) IsRevoked(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	jti string,
) (bool, error) {
	var revoked bool
	err := c.kv.Get(ctx, revocationKey(space, jti), &revoked)
	if errors.Is(err, keyval.ErrNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return revoked, nil
}

type SpaceCredentialAuthMethod struct {
	dir         identity.Directory
	revocations *CredentialRevocations
}

var _ Method = (*SpaceCredentialAuthMethod)(nil)

// NewSpaceCredentialAuthMethod verifies space credentials against their
// authority's DID document, refusing any revoked in revocations.
func NewSpaceCredentialAuthMethod(
	directory identity.Directory,
	revocations *CredentialRevocations,
) *SpaceCredentialAuthMethod {
	return &SpaceCredentialAuthMethod{dir: directory, revocations: revocations}
}

// CanHandle implements [Method].
func (s *SpaceCredentialAuthMethod) CanHandle(r *http.Request) bool {
	token, err := getBearerJwt(r)
	if err != nil {
		return false
	}
	return token.Header["typ"] == "atproto-space-credential+jwt"
}

// Validate implements [Method].
func (s *SpaceCredentialAuthMethod) Validate(
	w http.ResponseWriter,
	r *http.Request,
	scopes ...string,
) (*CredentialInfo, bool) {
	ctx := r.Context()
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(
		getBearerToken(r),
		claims,
		fetchIssuerKeyFunc(ctx, s.dir, nil),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(time.Second*10),
	)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse token", err)
		return nil, false
	}
	// A space authority always signs credentials with its #atproto key.
	if kid := token.Header["kid"]; kid != spaceCredentialKid {
		httpx.WriteInvalidRequest(
			ctx, w, "invalid token kid",
			fmt.Errorf("expected %q, got %v", spaceCredentialKid, kid),
		)
		return nil, false
	}
	space, err := getSpaceSubj(token.Claims)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid space in token", err)
		return nil, false
	}
	issuer, _ /* issuer must exist from verification */ := token.Claims.GetIssuer()

	if issuer != string(space.SpaceOwner()) {
		httpx.WriteInvalidRequest(ctx, w, "token issuer does not match space", err)
		return nil, false
	}

	audience, err := verifyKeyBinding(r, claims)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "credential key binding", err)
		return nil, false
	}
	// The signature binds the credential to an audience; it must be the one
	// this request addresses, or a host given the credential could replay it
	// against every other host in the space.
	if want := requestAudience(r, space); audience != want {
		httpx.WriteUnauthorized(
			ctx, w, "credential signed for another audience",
			fmt.Errorf("audience %q, request addresses %q", audience, want),
		)
		return nil, false
	}
	jti, _ := claims["jti"].(string)
	if jti == "" {
		httpx.WriteInvalidRequest(ctx, w, "space credential has no jti", nil)
		return nil, false
	}
	revoked, err := s.revocations.IsRevoked(ctx, space, jti)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("check credential revocation: %w", err))
		return nil, false
	} else if revoked {
		httpx.WriteError(ctx, w, "CredentialRevoked", "space credential was revoked", http.StatusUnauthorized)
		return nil, false
	}

	return &CredentialInfo{
		Space:    space,
		Audience: audience,
		Method:   ValidatorMethodSpaceCredential,
	}, true
}

// verifyKeyBinding enforces the credential's cnf claim: a space credential is
// bound to a key (cnf.kid, a P-256 did:key) and must be presented with an RFC
// 9421 signature by that key over the credential and the
// Atproto-Space-Audience header. The signed audience is returned.
func verifyKeyBinding(r *http.Request, claims jwt.MapClaims) (string, error) {
	cnfMap, ok := claims["cnf"].(map[string]any)
	if !ok {
		return "", errors.New("space credential must carry a cnf claim")
	}
	kid, ok := cnfMap["kid"].(string)
	if !ok || kid == "" {
		return "", errors.New("unsupported cnf claim: only cnf.kid is supported")
	}
	if !httpsig.IsP256DIDKey(kid) {
		return "", fmt.Errorf("cnf.kid %q is not a P-256 did:key", kid)
	}
	// The signature may omit keyid; it is checked against cnf.kid either way.
	if err := httpsig.VerifyBound(r, kid, "authorization", "atproto-space-audience"); err != nil {
		return "", err
	}
	audience := strings.TrimSpace(r.Header.Get(httpsig.AudienceHeader))
	if audience == "" {
		return "", errors.New("missing Atproto-Space-Audience header")
	}
	return audience, nil
}

// requestAudience is the DID a space credential request addresses: the repo
// owner for repo methods, which name it in the repo parameter, and otherwise
// the space authority, for requests against the space host.
func requestAudience(r *http.Request, space habitat_syntax.SpaceURI) string {
	if repo := r.URL.Query().Get("repo"); repo != "" {
		return repo
	}
	return string(space.SpaceOwner())
}
