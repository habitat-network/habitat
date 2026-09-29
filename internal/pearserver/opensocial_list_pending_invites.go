package pearserver

import (
	"fmt"
	"net/http"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
)

// ListPendingInvites implements community.opensocial.listPendingInvites.
func (p *PearServer) ListPendingInvites(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var params opensocial_api.CommunityOpensocialListPendingInvitesParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode query params", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, params.Org, "org")
	if !ok {
		return
	}
	if !p.requireAdmin(ctx, w, org, credInfo.Subject) {
		return
	}
	invites, err := p.opensocialStore.ListPendingInvites(ctx, org)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list pending invites: %w", err))
		return
	}
	views := make([]opensocial_api.CommunityOpensocialDefsInviteView, len(invites))
	for i, invite := range invites {
		views[i] = inviteToView(invite)
	}
	httpx.WriteJSON(
		ctx, w, opensocial_api.CommunityOpensocialListPendingInvitesOutput{Invites: views},
	)
}
