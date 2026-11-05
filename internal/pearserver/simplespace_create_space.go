package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/comatproto"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/simplespace"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// CreateSpace implements network.habitat.simplespace.createSpace and
// com.atproto.space.createSpace.
func (p *PearServer) CreateSpace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	// The two namespaces have their own generated inputs: network.habitat
	// carries a `config` this host ignores, com.atproto.space carries the
	// proposal's `policy`.
	var input habitat.NetworkHabitatSimplespaceCreateSpaceInput
	var rawPolicy json.RawMessage
	if httpx.IsComAtprotoRequest(r) {
		var in comatproto.ComAtprotoSpaceCreateSpaceInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
			return
		}
		var err error
		if rawPolicy, err = marshalPolicyInput(in.Policy); err != nil {
			httpx.WriteInvalidRequest(ctx, w, "policy is required", err)
			return
		}
		input.Type, input.Skey = in.Type, in.Skey
	} else if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	var policy spaces.Policy
	if httpx.IsComAtprotoRequest(r) {
		var err error
		if policy, err = spaces.ParsePolicy(rawPolicy); !writePolicyError(ctx, w, err) {
			return
		}
	} else {
		policy = spaces.DefaultPolicy()
	}
	spaceType, ok := httpx.ParseNSIDInput(ctx, w, input.Type, "space type")
	if !ok {
		return
	}
	var skey habitat_syntax.SpaceKey
	if input.Skey != "" {
		parsedKey, err := habitat_syntax.ParseSkey(input.Skey)
		if err != nil {
			httpx.WriteInvalidRequest(ctx, w, "invalid skey", err)
			return
		}
		skey = parsedKey
	}

	if input.Did != "" {
		parsedDID, ok := httpx.ParseDIDInput(ctx, w, input.Did, "did")
		if !ok {
			return
		}
		if parsedDID != credInfo.Subject {
			httpx.WriteInvalidRequest(ctx, w, "only the caller did is allowed", nil)
			return
		}
	}

	uri, err := p.simpleStore.CreateSpace(ctx, credInfo.Subject, spaceType, skey)
	if errors.Is(err, simplespace.ErrSpaceAlreadyExists) {
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
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSimplespaceCreateSpaceOutput{
		Uri: uri.String(),
	})
}
