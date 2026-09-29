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

// CreateInvite implements community.opensocial.createInvite.
func (p *PearServer) CreateInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.CommunityOpensocialCreateInviteInput
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
	invitee, ok := httpx.ParseDIDInput(ctx, w, input.Invitee, "invitee")
	if !ok {
		return
	}
	invite, err := p.opensocialStore.CreateInvite(ctx, org, invitee, input.Roles)
	if errors.Is(err, opensocial.ErrAlreadyMember) {
		httpx.WriteError(
			ctx, w, "AlreadyMember", "invitee is already a member", http.StatusBadRequest,
		)
		return
	}
	if errors.Is(err, opensocial.ErrInviteAlreadyExists) {
		httpx.WriteError(
			ctx, w, "InviteAlreadyExists",
			"invitee already has a pending invite", http.StatusBadRequest,
		)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("create invite: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, opensocial_api.CommunityOpensocialCreateInviteOutput{
		Invite: inviteToView(invite),
	})
}
