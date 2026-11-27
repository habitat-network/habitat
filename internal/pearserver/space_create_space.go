package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/comatproto"
	"github.com/habitat-network/habitat/internal/atgrp"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/simplespace"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// parseSkeyInput parses an optional space key, writing an invalid-request
// response and returning false on a malformed one. An empty key yields the
// zero SpaceKey, which the store replaces with a generated TID.
func parseSkeyInput(
	w http.ResponseWriter,
	r *http.Request,
	skey string,
) (habitat_syntax.SpaceKey, bool) {
	if skey == "" {
		return "", true
	}
	parsed, err := habitat_syntax.ParseSkey(skey)
	if err != nil {
		httpx.WriteInvalidRequest(r.Context(), w, "invalid skey", err)
		return "", false
	}
	return parsed, true
}

// CreateSpace implements com.atproto.space.createSpace: it creates a space
// owned by the host, makes the caller its owner, and stores the requested
// policy.
func (p *PearServer) CreateSpace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input comatproto.ComAtprotoSpaceCreateSpaceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	rawPolicy, err := marshalPolicyInput(input.Policy)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "policy is required", err)
		return
	}
	policy, err := spaces.ParsePolicy(rawPolicy)
	if !writePolicyError(ctx, w, err) {
		return
	}
	spaceType, ok := httpx.ParseNSIDInput(ctx, w, input.SpaceType, "space type")
	if !ok {
		return
	}
	skey, ok := parseSkeyInput(w, r, input.Skey)
	if !ok {
		return
	}

	var uri habitat_syntax.SpaceURI
	if spaceType == atgrp.InboxSpaceType {
		if skey != "self" || !atgrp.IsInboxPolicy(policy) {
			httpx.WriteInvalidRequest(ctx, w, "inbox requires key self and a private public-write memberspace policy", nil)
			return
		}
		if !p.requireLocalInboxAccount(w, r, credInfo.Subject) {
			return
		}
		// Unlike a Habitat-managed space, an inbox belongs to the personal
		// account itself. A group must not create an inbox for its invitee.
		uri, err = p.spacesStore.CreateSpace(ctx, credInfo.Subject, spaceType, skey)
	} else {
		uri, err = p.simpleStore.CreateSpace(ctx, credInfo.Subject, spaceType, skey)
	}
	if errors.Is(err, simplespace.ErrSpaceAlreadyExists) || errors.Is(err, spaces.ErrSpaceAlreadyExists) {
		httpx.WriteError(ctx, w, "SpaceAlreadyExists", "" /* msg */, http.StatusBadRequest)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("create space: %w", err))
		return
	}
	if err := p.spacesStore.SetSpacePolicy(ctx, uri, policy); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("set space policy: %w", err))
		return
	}
	httpx.WriteJSON(
		ctx,
		w,
		comatproto.ComAtprotoSpaceCreateSpaceOutput{Space: uri.AtSpace().String()},
	)
}
