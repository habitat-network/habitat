package pearserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/comatproto"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// writePolicyError reports whether err is nil, writing the matching proposal
// error (UnsupportedPolicy) or an invalid-request response otherwise.
func writePolicyError(ctx context.Context, w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, spaces.ErrUnsupportedPolicy):
		httpx.WriteError(ctx, w, "UnsupportedPolicy", err.Error(), http.StatusBadRequest)
	case errors.Is(err, spaces.ErrInvalidPolicy):
		httpx.WriteInvalidRequest(ctx, w, "invalid policy", err)
	default:
		httpx.WriteServerError(ctx, w, err)
	}
	return false
}

// UpdateSpace implements com.atproto.space.updateSpace: it replaces the
// space's policy wholesale.
//
// Unlike the proposal's memberspace, habitat's member list is the space's
// permission roles, which other features (orgs, relationships) also own, so a
// migration to another policy does not discard them.
func (p *PearServer) UpdateSpace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input struct {
		Space  string          `json:"space"`
		Policy json.RawMessage `json:"policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	if len(input.Policy) == 0 {
		httpx.WriteInvalidRequest(ctx, w, "policy is required", nil)
		return
	}
	if _, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleOwner),
	).Validate(w, r); !ok {
		return
	}
	policy, err := spaces.ParsePolicy(input.Policy)
	if !writePolicyError(ctx, w, err) {
		return
	}
	err = p.spacesStore.SetSpacePolicy(ctx, spaceURI, policy)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("update space: %w", err))
		return
	}
}

// GetSpace implements com.atproto.space.getSpace.
func (p *PearServer) GetSpace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params comatproto.ComAtprotoSpaceGetSpaceParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space uri")
	if !ok {
		return
	}
	if _, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleReader),
	).Validate(w, r); !ok {
		return
	}
	policy, err := p.spacesStore.GetSpacePolicy(ctx, spaceURI)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("get space: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, comatproto.ComAtprotoSpaceGetSpaceOutput{
		Uri: spaceURI.String(), Policy: policy,
	})
}

// requireMemberspace writes an error and returns false unless the space uses
// the memberspace policy, the only one with a member list.
func (p *PearServer) requireMemberspace(
	ctx context.Context,
	w http.ResponseWriter,
	uri habitat_syntax.SpaceURI,
) bool {
	policy, err := p.spacesStore.GetSpacePolicy(ctx, uri)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return false
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("get space policy: %w", err))
		return false
	}
	if policy.Type != spaces.PolicyMemberSpace {
		httpx.WriteInvalidRequest(ctx, w, "space does not use the memberspace policy", nil)
		return false
	}
	return true
}
