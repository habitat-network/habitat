package pearserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/opensocial"
)

const (
	defaultJoinRequestLimit = 50
	maxJoinRequestLimit     = 100
)

// groupCaller is who acts on a group and what they may do right now.
type groupCaller struct {
	Group syntax.DID
	// Actor is the member the call is made as: the caller themselves, or for a
	// session acting as the group, the member who authorized it.
	Actor  syntax.DID
	Scopes opensocial.ScopeSet
}

// callerFor resolves who credInfo acts as in group, and with which scopes.
func (p *PearServer) callerFor(
	ctx context.Context,
	w http.ResponseWriter,
	credInfo *authn.CredentialInfo,
	group syntax.DID,
) (groupCaller, bool) {
	actor, scopes, err := p.opensocialStore.SessionScopes(
		ctx, group, credInfo.Subject, credInfo.Authorizer, credInfo.Scopes,
	)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("session scopes: %w", err))
		return groupCaller{}, false
	}
	return groupCaller{Group: group, Actor: actor, Scopes: scopes}, true
}

// authenticateGroup validates a method that only the group itself may call,
// through an OAuth session acting as the group, and checks the session may
// call lxm right now.
func (p *PearServer) authenticateGroup(
	w http.ResponseWriter,
	r *http.Request,
	lxm string,
) (groupCaller, bool) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
	if !ok {
		return groupCaller{}, false
	}
	group, ok := p.isGroup(ctx, w, credInfo.Subject)
	if !ok {
		return groupCaller{}, false
	}
	if !group {
		httpx.WriteUnauthorized(ctx, w, "only a group may call this method", nil)
		return groupCaller{}, false
	}
	caller, ok := p.callerFor(ctx, w, credInfo, credInfo.Subject)
	if !ok {
		return groupCaller{}, false
	}
	if !caller.Scopes.Covers("rpc:" + lxm) {
		httpx.WriteUnauthorized(ctx, w, "the session may not call "+lxm, nil)
		return groupCaller{}, false
	}
	return caller, true
}

// GroupListJoinRequests implements org.atgrp.listJoinRequests.
func (p *PearServer) GroupListJoinRequests(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	caller, ok := p.authenticateGroup(w, r, "org.atgrp.listJoinRequests")
	if !ok {
		return
	}
	group := caller.Group
	var params opensocial_api.OrgAtgrpListJoinRequestsParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode query", err)
		return
	}
	limit := int(params.Limit)
	switch {
	case limit == 0:
		limit = defaultJoinRequestLimit
	case limit < 1 || limit > maxJoinRequestLimit:
		httpx.WriteInvalidRequest(ctx, w, "limit out of range", nil)
		return
	}
	requests, next, err := p.opensocialStore.ListJoinRequests(ctx, group, limit, params.Cursor)
	if errors.Is(err, opensocial.ErrInvalidCursor) {
		httpx.WriteError(ctx, w, "InvalidCursor", "", http.StatusBadRequest)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list join requests: %w", err))
		return
	}
	out := opensocial_api.OrgAtgrpListJoinRequestsOutput{
		Cursor:   next,
		Requests: make([]opensocial_api.OrgAtgrpDefsJoinRequest, 0, len(requests)),
	}
	for _, request := range requests {
		out.Requests = append(out.Requests, joinRequestView(request))
	}
	httpx.WriteJSON(ctx, w, out)
}

// GroupDenyJoinRequest implements org.atgrp.denyJoinRequest.
func (p *PearServer) GroupDenyJoinRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	caller, ok := p.authenticateGroup(w, r, "org.atgrp.denyJoinRequest")
	if !ok {
		return
	}
	group := caller.Group
	var input opensocial_api.OrgAtgrpDenyJoinRequestInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	member, ok := httpx.ParseDIDInput(ctx, w, input.Member, "member")
	if !ok {
		return
	}
	p.writeJoinRequestDelete(w, r, group, member)
}

// GroupCancelJoinRequest implements org.atgrp.cancelJoinRequest.
func (p *PearServer) GroupCancelJoinRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.OrgAtgrpCancelJoinRequestInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	group, ok := httpx.ParseDIDInput(ctx, w, input.Group, "group")
	if !ok {
		return
	}
	isGroup, ok := p.isGroup(ctx, w, group)
	if !ok {
		return
	}
	if !isGroup {
		httpx.WriteError(ctx, w, "GroupNotFound", "", http.StatusNotFound)
		return
	}
	p.writeJoinRequestDelete(w, r, group, credInfo.Subject)
}

func (p *PearServer) writeJoinRequestDelete(
	w http.ResponseWriter,
	r *http.Request,
	group, member syntax.DID,
) {
	ctx := r.Context()
	err := p.opensocialStore.DeleteJoinRequest(ctx, group, member)
	switch {
	case errors.Is(err, opensocial.ErrJoinRequestNotFound):
		httpx.WriteError(ctx, w, "JoinRequestNotFound", "", http.StatusBadRequest)
	case err != nil:
		httpx.WriteServerError(ctx, w, fmt.Errorf("delete join request: %w", err))
	}
}
