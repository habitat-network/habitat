package pearserver

import (
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
)

// ListInvites implements network.habitat.opensocial.listInvites.
func (p *PearServer) ListInvites(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	invites, err := p.opensocialStore.ListInvites(ctx, credInfo.Subject)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list my invites: %w", err))
		return
	}
	views := make([]habitat.NetworkHabitatOpensocialDefsInviteView, len(invites))
	for i, invite := range invites {
		views[i] = inviteToView(invite)
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatOpensocialListInvitesOutput{Invites: views})
}
