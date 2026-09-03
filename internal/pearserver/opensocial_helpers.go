package pearserver

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/opensocial"
)

func inviteToView(invite opensocial.Invite) opensocial_api.CommunityOpensocialDefsInviteView {
	return opensocial_api.CommunityOpensocialDefsInviteView{
		Id:        invite.ID,
		Org:       invite.Org.String(),
		Invitee:   invite.Invitee.String(),
		Roles:     invite.Roles,
		CreatedAt: invite.CreatedAt.Format(time.RFC3339),
	}
}

// requireAdmin validates that caller holds the community's admin role,
// writing an appropriate error response and returning false if not.
func (p *PearServer) requireAdmin(
	ctx context.Context,
	w http.ResponseWriter,
	org syntax.DID,
	caller syntax.DID,
) bool {
	roles, err := p.opensocialStore.GetUserRoles(ctx, org, caller)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("get user roles: %w", err))
		return false
	}
	if !slices.Contains(roles, opensocial.AdminRoleRkey) {
		httpx.WriteUnauthorized(ctx, w, "caller is not an admin of this community")
		return false
	}
	return true
}
