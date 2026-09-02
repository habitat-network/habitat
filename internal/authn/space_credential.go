package authn

import (
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
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
	token, err := jwt.ParseWithClaims(
		getBearerToken(r),
		jwt.MapClaims{},
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

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		httpx.WriteInvalidRequest(ctx, w, "invalid token claims", nil)
		return nil, false
	}
	cnf, ok := claims["cnf"].(map[string]any)
	if !ok {
		httpx.WriteInvalidRequest(ctx, w, "token missing cnf claim", nil)
		return nil, false
	}
	jkt, ok := cnf["jkt"].(string)
	if !ok || jkt == "" {
		httpx.WriteInvalidRequest(ctx, w, "token missing cnf.jkt claim", nil)
		return nil, false
	}

	// The credential is presented as a DPoP-bound token (RFC 9449): the
	// caller must prove, with each request, possession of the key the
	// credential's `cnf.jkt` was minted against.
	proofJKT, err := verifyDPoPProof(r, getBearerToken(r))
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid DPoP proof", err)
		return nil, false
	}
	if proofJKT != jkt {
		httpx.WriteInvalidRequest(ctx, w, "DPoP proof key does not match credential", nil)
		return nil, false
	}

	return &CredentialInfo{
		Space: space,
	}, true
}
