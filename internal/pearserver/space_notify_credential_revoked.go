package pearserver

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
)

// NotifyCredentialRevoked implements com.atproto.space.notifyCredentialRevoked
// on the repo host: the space's authority reports outstanding credentials,
// identified by jti, as revoked. Authenticated with service auth from the space
// authority, so only the space's owner can revoke its credentials. Revocation
// is idempotent, so the authority may retry delivery.
func (p *PearServer) NotifyCredentialRevoked(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceNotifyCredentialRevokedInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	if credInfo.Subject != spaceURI.SpaceOwner() {
		httpx.WriteUnauthorized(ctx, w, "only the space authority can revoke credentials", nil)
		return
	}
	if len(input.Jtis) == 0 {
		httpx.WriteInvalidRequest(ctx, w, "jtis is required", nil)
		return
	}
	if err := p.revocations.Revoke(ctx, spaceURI, input.Jtis); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("revoke credentials: %w", err))
		return
	}
}
