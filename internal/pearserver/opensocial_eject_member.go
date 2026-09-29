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

// EjectMember implements community.opensocial.ejectMember, bounded by the
// roles credInfo.Subject may assign: it may only eject a member all of
// whose current roles it's permitted to assign.
func (p *PearServer) EjectMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.CommunityOpensocialEjectMemberInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	member, ok := httpx.ParseDIDInput(ctx, w, input.Member, "member")
	if !ok {
		return
	}
	if !p.requireAction(ctx, w, org, credInfo.Subject, opensocial.ActionEject) {
		return
	}
	currentRoles, err := p.opensocialStore.GetUserRoles(ctx, org, member)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("get user roles: %w", err))
		return
	}
	if len(currentRoles) == 0 {
		httpx.WriteError(ctx, w, "MemberNotFound", "", http.StatusBadRequest)
		return
	}
	canAssign, err := p.opensocialStore.CanAssignRoles(
		ctx, org, credInfo.Subject, currentRoles, nil,
	)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("can assign roles: %w", err))
		return
	}
	if !canAssign {
		httpx.WriteError(
			ctx, w, "RoleNotAssignable",
			"caller is not permitted to eject a member holding one of these roles",
			http.StatusForbidden,
		)
		return
	}
	err = p.opensocialStore.EjectMember(ctx, org, member)
	if errors.Is(err, opensocial.ErrMemberNotFound) {
		httpx.WriteError(ctx, w, "MemberNotFound", "", http.StatusBadRequest)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("eject member: %w", err))
		return
	}
}
