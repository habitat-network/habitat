package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/comatproto"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/opensocial"
)

func strongRefOutput(ref opensocial.StrongRef) comatproto.ComAtprotoRepoStrongRefMain {
	return comatproto.ComAtprotoRepoStrongRefMain{Uri: ref.URI, Cid: ref.CID}
}

// groupActor validates the caller of a group-DID method. The group acts
// through an OAuth credential for its own DID; a member acts through service
// auth. The store decides, from the caller's scopes, what they may do.
func (p *PearServer) groupActor(
	w http.ResponseWriter,
	r *http.Request,
	group *syntax.DID,
) (syntax.DID, bool) {
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return "", false
	}
	if credInfo.Method == authn.ValidatorMethodOAuth && group != nil && credInfo.Subject != *group {
		httpx.WriteUnauthorized(
			r.Context(), w, "only the group's own OAuth credential may do this", nil,
		)
		return "", false
	}
	return credInfo.Subject, true
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
		httpx.WriteError(
			ctx, w, "NotImplemented",
			opensocial.ErrJoinNeedsApproval.Error(), http.StatusNotImplemented,
		)
	default:
		httpx.WriteError(
			ctx,
			w,
			"JoinClosed",
			opensocial.ErrJoinClosed.Error(),
			http.StatusForbidden,
		)
	}
}
