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
	"github.com/habitat-network/habitat/internal/spaces"
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

// GroupCreateSpace implements org.atgrp.createSpace.
func (p *PearServer) GroupCreateSpace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input struct {
		opensocial_api.OrgAtgrpCreateSpaceInput
		// Index defaults to true when omitted.
		Index *bool `json:"index"`
		// Group names the group to create the space in. The spec leaves it
		// implicit (a group-DID session); a member calling with service auth
		// must name it, and a group's own OAuth session may omit it.
		Group string `json:"group"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	var named *syntax.DID
	if input.Group != "" {
		group, ok := httpx.ParseDIDInput(ctx, w, input.Group, "group")
		if !ok {
			return
		}
		named = &group
	}
	actor, ok := p.groupActor(w, r, named)
	if !ok {
		return
	}
	group := actor
	if named != nil {
		group = *named
	}
	spaceType, ok := httpx.ParseNSIDInput(ctx, w, input.SpaceType, "spaceType")
	if !ok {
		return
	}
	skey, ok := parseSkeyInput(w, r, input.Skey)
	if !ok {
		return
	}
	created, err := p.opensocialStore.CreateGroupSpace(
		ctx, group, actor, spaceType, skey,
		spaces.Policy{Type: spaces.PolicyRelationSpace},
		input.Access, input.Index == nil || *input.Index,
	)
	switch {
	case errors.Is(err, spaces.ErrSpaceAlreadyExists):
		httpx.WriteError(ctx, w, "SpaceAlreadyExists", "", http.StatusBadRequest)
		return
	case errors.Is(err, opensocial.ErrReservedSpace):
		httpx.WriteError(ctx, w, "ReservedSpace", err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, opensocial.ErrUnknownRole):
		httpx.WriteError(ctx, w, "UnknownRole", err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, opensocial.ErrInvalidScope):
		httpx.WriteError(ctx, w, "InvalidScope", err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, opensocial.ErrEscalation):
		httpx.WriteUnauthorized(ctx, w, err.Error(), nil)
		return
	case err != nil:
		httpx.WriteServerError(ctx, w, fmt.Errorf("create group space: %w", err))
		return
	}
	out := opensocial_api.OrgAtgrpCreateSpaceOutput{
		Space:  created.Space.AtSpace().String(),
		Access: strongRefOutput(created.Access),
	}
	if created.Index != nil {
		ref := strongRefOutput(*created.Index)
		out.Index = &ref
	}
	httpx.WriteJSON(ctx, w, out)
}

// GroupDeleteSpace implements org.atgrp.deleteSpace.
func (p *PearServer) GroupDeleteSpace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input opensocial_api.OrgAtgrpDeleteSpaceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space")
	if !ok {
		return
	}
	group := spaceURI.SpaceOwner()
	actor, ok := p.groupActor(w, r, &group)
	if !ok {
		return
	}
	err := p.opensocialStore.DeleteGroupSpace(ctx, group, actor, spaceURI)
	switch {
	case errors.Is(err, spaces.ErrSpaceNotFound):
		httpx.WriteError(ctx, w, "SpaceNotFound", "", http.StatusBadRequest)
	case errors.Is(err, opensocial.ErrReservedSpace):
		httpx.WriteError(ctx, w, "ReservedSpace", err.Error(), http.StatusBadRequest)
	case errors.Is(err, opensocial.ErrEscalation):
		httpx.WriteUnauthorized(ctx, w, err.Error(), nil)
	case err != nil:
		httpx.WriteServerError(ctx, w, fmt.Errorf("delete group space: %w", err))
	}
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
