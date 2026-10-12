package authn

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/habitat-network/habitat/internal/httpsig"
	"github.com/habitat-network/habitat/internal/httpx"

	"github.com/golang-jwt/jwt/v5"
)

type SpaceCredentialAuthMethod struct {
	dir identity.Directory
}

var _ Method = (*SpaceCredentialAuthMethod)(nil)

func NewSpaceCredentialAuthMethod(
	directory identity.Directory,
) *SpaceCredentialAuthMethod {
	return &SpaceCredentialAuthMethod{dir: directory}
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

	return &CredentialInfo{
		Space:    space,
		Audience: audience,
		Method:   ValidatorMethodSpaceCredential,
	}, true
}

// verifyKeyBinding enforces the credential's cnf claim. A credential bound to
// a key (cnf.kid, a P-256 did:key) must be presented with an RFC 9421
// signature by that key over the credential and the Atproto-Space-Audience
// header; the audience is returned. A credential with no cnf claim is the
// legacy unbound form and is accepted as before.
func verifyKeyBinding(r *http.Request, claims jwt.MapClaims) (string, error) {
	cnf, hasCnf := claims["cnf"]
	if !hasCnf {
		return "", nil
	}
	cnfMap, ok := cnf.(map[string]any)
	if !ok {
		return "", errors.New("malformed cnf claim")
	}
	kid, ok := cnfMap["kid"].(string)
	if !ok || kid == "" {
		return "", errors.New("unsupported cnf claim: only cnf.kid is supported")
	}
	// The signature may omit keyid; it is checked against cnf.kid either way.
	if err := httpsig.VerifyBound(r, kid, "authorization", "atproto-space-audience"); err != nil {
		return "", err
	}
	return strings.TrimSpace(r.Header.Get(httpsig.AudienceHeader)), nil
}
