package authn

import (
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/httpx"
)

type DelegationTokenAuthMethod struct {
	dir identity.Directory
	fga fgastore.Store
}

var _ Method = (*DelegationTokenAuthMethod)(nil)

func NewDelegationTokenAuthMethod(directory identity.Directory) *DelegationTokenAuthMethod {
	return &DelegationTokenAuthMethod{
		dir: directory,
	}
}

// CanHandle implements [Method].
func (d *DelegationTokenAuthMethod) CanHandle(r *http.Request) bool {
	token, err := getBearerJwt(r)
	if err != nil {
		return false
	}
	return token.Header["typ"] == "atproto-space-delegation+jwt"
}

// Validate implements [Method].
func (d *DelegationTokenAuthMethod) Validate(
	w http.ResponseWriter,
	r *http.Request,
	scopes ...string,
) (*CredentialInfo, bool) {
	ctx := r.Context()
	token, err := jwt.ParseWithClaims(
		getBearerToken(r),
		jwt.MapClaims{},
		fetchIssuerKeyFunc(ctx, d.dir),
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
	allowed, err := d.fga.Check(
		ctx,
		fgastore.MemberUserString(syntax.DID(issuer)),
		fgastore.RelationMember,
		fgastore.SpaceObjectKey(space),
	)
	if err != nil {
		httpx.WriteServerError(ctx, w, err)
		return nil, false
	}
	if !allowed {
		httpx.WriteError(ctx, w, "UserNotAuthorized", "", http.StatusBadRequest)
		return nil, false
	}
	return &CredentialInfo{
		Space: space,
	}, true
}
