package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

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
	// One input shape serves both namespaces: network.habitat.simplespace
	// carries a `config` this host ignores, com.atproto.space carries the
	// proposal's `policy`.
	var input struct {
		Did    string          `json:"did"`
		Type   string          `json:"type"`
		Skey   string          `json:"skey"`
		Policy json.RawMessage `json:"policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	var policy spaces.Policy
	if httpx.IsComAtprotoRequest(r) {
		if len(input.Policy) == 0 {
			httpx.WriteInvalidRequest(ctx, w, "policy is required", nil)
			return
		}
		var err error
		if policy, err = spaces.ParsePolicy(input.Policy); !writePolicyError(ctx, w, err) {
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

	callerOrg := credInfo.Org.DID()
	if input.Did != "" {
		parsedDID, ok := httpx.ParseDIDInput(ctx, w, input.Did, "did")
		if !ok {
			return
		}
		if parsedDID != credInfo.Subject && parsedDID != callerOrg {
			httpx.WriteInvalidRequest(ctx, w, "only caller did or caller org are allowed", nil)
			return
		}
	}

	uri, err := p.simpleStore.CreateSpace(ctx, callerOrg, credInfo.Subject, spaceType, skey)
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
