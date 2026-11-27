package pearserver

import (
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

// authenticateGroup validates a method that only the group itself may call,
// through its own OAuth credential, and returns the group DID.
func (p *PearServer) authenticateGroup(
	w http.ResponseWriter,
	r *http.Request,
) (syntax.DID, bool) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
	if !ok {
		return "", false
	}
	group, ok := p.isGroup(ctx, w, credInfo.Subject)
	if !ok {
		return "", false
	}
	if !group {
		httpx.WriteUnauthorized(ctx, w, "only a group may call this method", nil)
		return "", false
	}
	return credInfo.Subject, true
}

// GroupListJoinRequests implements org.atgrp.listJoinRequests.
func (p *PearServer) GroupListJoinRequests(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	group, ok := p.authenticateGroup(w, r)
	if !ok {
		return
	}
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
	group, ok := p.authenticateGroup(w, r)
	if !ok {
		return
	}
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
