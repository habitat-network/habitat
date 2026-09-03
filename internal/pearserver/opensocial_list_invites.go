package pearserver

import (
	"fmt"
	"net/http"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
)

// ListInvites implements community.opensocial.listInvites.
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
	views := make([]opensocial_api.CommunityOpensocialDefsInviteView, len(invites))
	for i, invite := range invites {
		views[i] = inviteToView(invite)
	}
	httpx.WriteJSON(ctx, w, opensocial_api.CommunityOpensocialListInvitesOutput{Invites: views})
}
