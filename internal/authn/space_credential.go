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

func NewSpaceCredentialAuthMethod(directory identity.Directory) *SpaceCredentialAuthMethod {
	return &SpaceCredentialAuthMethod{
		dir: directory,
	}
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
		fetchIssuerKeyFunc(ctx, s.dir),
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

	return &CredentialInfo{
		Space: space,
	}, true
}
