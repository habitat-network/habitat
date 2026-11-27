package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"


	"github.com/habitat-network/habitat/api/comatproto"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/opensocial"
)

func strongRefOutput(ref opensocial.StrongRef) comatproto.ComAtprotoRepoStrongRefMain {
	return comatproto.ComAtprotoRepoStrongRefMain{Uri: ref.URI, Cid: ref.CID}
}

// GroupLeaveGroup implements org.atgrp.leaveGroup.
func (p *PearServer) GroupLeaveGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.OrgAtgrpLeaveGroupInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	group, ok := httpx.ParseDIDInput(ctx, w, input.Group, "group")
	if !ok {
		return
	}
	err := p.opensocialStore.LeaveGroup(ctx, group, credInfo.Subject)
	switch {
	case errors.Is(err, opensocial.ErrGroupNotFound):
		httpx.WriteError(ctx, w, "GroupNotFound", "", http.StatusNotFound)
	case err != nil:
		httpx.WriteServerError(ctx, w, fmt.Errorf("leave group: %w", err))
	}
}

// GroupRequestJoin implements org.atgrp.requestJoin: an invitation
// admits its holder under any join policy, an open group admits anyone, and
// an invite-only group otherwise refuses.
func (p *PearServer) GroupRequestJoin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.OrgAtgrpRequestJoinInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	group, ok := httpx.ParseDIDInput(ctx, w, input.Group, "group")
	if !ok {
		return
	}
	policy, err := p.opensocialStore.JoinPolicy(ctx, group)
	if errors.Is(err, opensocial.ErrGroupNotFound) {
		httpx.WriteError(ctx, w, "GroupNotFound", "", http.StatusNotFound)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("join policy: %w", err))
		return
	}
	// An invitation admits its recipient whatever the policy.
	_, err = p.opensocialStore.RequestJoin(ctx, group, credInfo.Subject)
	switch {
	case err == nil:
		httpx.WriteJSON(ctx, w, opensocial_api.OrgAtgrpRequestJoinOutput{Status: "admitted"})
		return
	case !errors.Is(err, opensocial.ErrInviteNotFound):
		httpx.WriteServerError(ctx, w, fmt.Errorf("redeem invite: %w", err))
		return
	case input.Invite != "":
		httpx.WriteError(ctx, w, "InvalidInvite", "no such invite", http.StatusBadRequest)
		return
	}
	// A member, or a requester already waiting, gets their standing back.
	if roles, err := p.opensocialStore.GetUserRoles(ctx, group, credInfo.Subject); err == nil &&
		len(roles) > 0 {
		httpx.WriteJSON(ctx, w, opensocial_api.OrgAtgrpRequestJoinOutput{Status: "admitted"})
		return
	}
	if existing, err := p.opensocialStore.GetJoinRequest(ctx, group, credInfo.Subject); err == nil {
		httpx.WriteJSON(ctx, w, pendingJoinOutput(existing))
		return
	}
	switch policy {
	case "open":
		ref, _, err := p.opensocialStore.Admit(ctx, group, credInfo.Subject)
		if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("admit: %w", err))
			return
		}
		out := opensocial_api.OrgAtgrpRequestJoinOutput{Status: "admitted"}
		if ref.URI != "" {
			membership := strongRefOutput(ref)
			out.Membership = &membership
		}
		httpx.WriteJSON(ctx, w, out)
	case "approval":
		request, err := p.opensocialStore.QueueJoinRequest(
			ctx, group, credInfo.Subject, input.Message,
		)
		if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("queue join request: %w", err))
			return
		}
		httpx.WriteJSON(ctx, w, pendingJoinOutput(request))
	default:
		httpx.WriteError(
			ctx, w, "JoinClosed", opensocial.ErrJoinClosed.Error(), http.StatusForbidden,
		)
	}
}

func joinRequestView(request opensocial.JoinRequest) opensocial_api.OrgAtgrpDefsJoinRequest {
	return opensocial_api.OrgAtgrpDefsJoinRequest{
		Member:    request.Member.String(),
		Message:   request.Message,
		CreatedAt: request.CreatedAt.Format(time.RFC3339),
	}
}

func pendingJoinOutput(request opensocial.JoinRequest) opensocial_api.OrgAtgrpRequestJoinOutput {
	view := joinRequestView(request)
	return opensocial_api.OrgAtgrpRequestJoinOutput{Status: "pending", Request: &view}
}
