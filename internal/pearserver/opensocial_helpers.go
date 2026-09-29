package pearserver

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
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
		httpx.WriteUnauthorized(ctx, w, "caller is not an admin of this community", nil)
		return false
	}
	return true
}

// requireMember validates that caller holds any role in the community,
// writing an appropriate error response and returning false if not.
func (p *PearServer) requireMember(
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
	if len(roles) == 0 {
		httpx.WriteUnauthorized(ctx, w, "caller is not a member of this community", nil)
		return false
	}
	return true
}

// requireSpaceAuthority validates that credInfo may create a space under org,
// writing an appropriate error response and returning false if not. The org
// acts through OAuth, as an admin-approved app holding a credential for the
// org's own identity; members act through service auth, since Atproto-Proxy
// re-signs a member's session as a service-auth token for that member's DID.
// A member's own OAuth credential is not accepted — org spaces come from the
// org's credential, or from a member's through the proxy.
func (p *PearServer) requireSpaceAuthority(
	ctx context.Context,
	w http.ResponseWriter,
	org syntax.DID,
	credInfo *authn.CredentialInfo,
) bool {
	if credInfo.Method == authn.ValidatorMethodOAuth {
		if credInfo.Subject != org {
			httpx.WriteUnauthorized(
				ctx, w, "only the organization's own OAuth credential may do this", nil,
			)
			return false
		}
		return true
	}
	return p.requireMember(ctx, w, org, credInfo.Subject)
}

// requireAction validates that caller is authorized to perform action in
// org — i.e. holds a role bound to it in the community's
// community.opensocial.permissions record — writing an appropriate error
// response and returning false if not.
func (p *PearServer) requireAction(
	ctx context.Context,
	w http.ResponseWriter,
	org syntax.DID,
	caller syntax.DID,
	action opensocial.Action,
) bool {
	ok, err := p.opensocialStore.CheckAction(ctx, org, caller, action)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("check action: %w", err))
		return false
	}
	if !ok {
		httpx.WriteUnauthorized(
			ctx, w,
			fmt.Sprintf("caller is not authorized to perform the %q action", action),
			nil,
		)
		return false
	}
	return true
}
