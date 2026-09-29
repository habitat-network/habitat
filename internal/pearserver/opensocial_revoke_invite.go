package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/opensocial"
)

// RevokeInvite implements community.opensocial.revokeInvite.
func (p *PearServer) RevokeInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.CommunityOpensocialRevokeInviteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	if !p.requireAdmin(ctx, w, org, credInfo.Subject) {
		return
	}
	if err := p.opensocialStore.RevokeInvite(ctx, org, input.Id); err != nil {
		if errors.Is(err, opensocial.ErrInviteNotFound) {
			httpx.WriteError(
				ctx, w, "InviteNotFound",
				"no pending invite exists with this id", http.StatusNotFound,
			)
			return
		}
		httpx.WriteServerError(ctx, w, fmt.Errorf("revoke invite: %w", err))
		return
	}
}
