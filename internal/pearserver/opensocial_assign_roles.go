package pearserver

import (
	"encoding/json"
	"fmt"
	"net/http"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/opensocial"
)

// AssignRoles implements community.opensocial.assignRoles: sets member's
// full role set, bounded by the roles credInfo.Subject may assign per the
// community's permissions record.
func (p *PearServer) AssignRoles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.CommunityOpensocialAssignRolesInput
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
	if !p.requireAction(ctx, w, org, credInfo.Subject, opensocial.ActionRoleAssign) {
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
		ctx, org, credInfo.Subject, currentRoles, input.Roles,
	)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("can assign roles: %w", err))
		return
	}
	if !canAssign {
		httpx.WriteError(
			ctx, w, "RoleNotAssignable",
			"caller is not permitted to assign or revoke one of the given roles",
			http.StatusForbidden,
		)
		return
	}
	if err := p.opensocialStore.AssignRoles(ctx, org, member, input.Roles); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("assign roles: %w", err))
		return
	}
}
