package authn

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/perms"
)

type DelegationTokenAuthMethod struct {
	dir     identity.Directory
	ps      perms.Store
	hostKey atcrypto.PrivateKey
}

var _ Method = (*DelegationTokenAuthMethod)(nil)

func NewDelegationTokenAuthMethod(
	directory identity.Directory,
	ps perms.Store,
	hostKey atcrypto.PrivateKey,
) *DelegationTokenAuthMethod {
	return &DelegationTokenAuthMethod{
		dir:     directory,
		ps:      ps,
		hostKey: hostKey,
	}
}

// CanHandle implements [Method].
func (d *DelegationTokenAuthMethod) CanHandle(r *http.Request) bool {
	token, err := getBearerJwt(r)
	if err != nil {
		slog.WarnContext(r.Context(), "failed to get token", "err", err)
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
		fetchIssuerKeyFunc(ctx, d.dir, d.hostKey),
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
	allowed, err := d.ps.CheckUserHasSpaceRole(
		ctx,
		syntax.DID(issuer),
		space,
		perms.SpaceRoleReader,
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
