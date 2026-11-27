package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/comatproto"
	"github.com/habitat-network/habitat/internal/atgrp"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/simplespace"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// maxMembersPerCall is the per-request limit on putMembers and removeMembers.
const maxMembersPerCall = 100

// PutMembers implements com.atproto.memberspace.putMembers: it adds members to
// a space with a memberspace policy.
//
// Membership here is a single flag that grants read and write together (see
// ListMembers), so a member whose read or write is false cannot be
// represented and the whole request is refused before anything is applied.
func (p *PearServer) PutMembers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input comatproto.ComAtprotoMemberspacePutMembersInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := p.authorizeMemberChange(w, r, input.Space)
	if !ok {
		return
	}
	if len(input.Members) > maxMembersPerCall {
		httpx.WriteInvalidRequest(ctx, w, "too many members", nil)
		return
	}
	dids := make([]syntax.DID, len(input.Members))
	for i, m := range input.Members {
		if !m.Read || !m.Write {
			httpx.WriteInvalidRequest(
				ctx, w, "members must have both read and write access", nil,
			)
			return
		}
		if dids[i], ok = httpx.ParseDIDInput(ctx, w, m.Did, "did"); !ok {
			return
		}
	}
	for _, did := range dids {
		err := p.simpleStore.AddMember(ctx, spaceURI, did)
		if errors.Is(err, spaces.ErrUserAlreadyMember) {
			continue
		} else if errors.Is(err, spaces.ErrSpaceNotFound) {
			httpx.WriteSpaceNotFound(ctx, w, err)
			return
		} else if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("add member: %w", err))
			return
		}
	}
}

// RemoveMembers implements com.atproto.memberspace.removeMembers. DIDs that
// are not members are ignored.
func (p *PearServer) RemoveMembers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input comatproto.ComAtprotoMemberspaceRemoveMembersInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := p.authorizeMemberChange(w, r, input.Space)
	if !ok {
		return
	}
	if len(input.Dids) > maxMembersPerCall {
		httpx.WriteInvalidRequest(ctx, w, "too many dids", nil)
		return
	}
	dids := make([]syntax.DID, len(input.Dids))
	for i, d := range input.Dids {
		if dids[i], ok = httpx.ParseDIDInput(ctx, w, d, "did"); !ok {
			return
		}
	}
	for _, did := range dids {
		err := p.simpleStore.RemoveMember(ctx, spaceURI, did)
		if errors.Is(err, spaces.ErrNotAMember) {
			continue
		} else if errors.Is(err, spaces.ErrSpaceNotFound) {
			httpx.WriteSpaceNotFound(ctx, w, err)
			return
		} else if errors.Is(err, simplespace.ErrCannotRemoveOrg) {
			httpx.WriteInvalidRequest(ctx, w, "cannot remove org", err)
			return
		} else if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("remove member: %w", err))
			return
		}
	}
}

// authorizeMemberChange checks the caller manages the space and that the space
// has a memberspace policy, answering PolicyMismatch when it does not.
func (p *PearServer) authorizeMemberChange(
	w http.ResponseWriter,
	r *http.Request,
	space string,
) (habitat_syntax.SpaceURI, bool) {
	ctx := r.Context()
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, space, "space uri")
	if !ok {
		return "", false
	}
	if spaceURI.SpaceType() == atgrp.InboxSpaceType {
		httpx.WriteInvalidRequest(ctx, w, "an inbox has no members", nil)
		return "", false
	}
	if _, ok = p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleManager),
	).Validate(w, r); !ok {
		return "", false
	}
	policy, err := p.spacesStore.GetSpacePolicy(ctx, spaceURI)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return "", false
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("get space policy: %w", err))
		return "", false
	}
	if policy.Type != spaces.PolicyMemberSpace {
		httpx.WriteError(
			ctx, w, "PolicyMismatch",
			"the space does not have a memberspace policy", http.StatusBadRequest,
		)
		return "", false
	}
	return spaceURI, true
}
