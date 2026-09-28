package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"

	"github.com/habitat-network/habitat/internal/spaces"
)

// CreateOpensocialSpace implements community.opensocial.createSpace: creates
// a modality-specific space under the community DID, indexed with a
// community.opensocial.space record and readable by the given roles (see
// opensocial.Store.CreateSpace). The org itself may call this with OAuth, and
// a member may call it with service auth — see requireSpaceCreator.
func (p *PearServer) CreateOpensocialSpace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.CommunityOpensocialCreateSpaceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	if !p.requireSpaceCreator(ctx, w, org, credInfo) {
		return
	}
	spaceType, ok := httpx.ParseNSIDInput(ctx, w, input.Type, "type")
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
	uri, err := p.opensocialStore.CreateSpace(ctx, org, input.Roles, spaceType, skey)
	if errors.Is(err, spaces.ErrSpaceAlreadyExists) {
		httpx.WriteError(ctx, w, "SpaceAlreadyExists", "", http.StatusBadRequest)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("create space: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, opensocial_api.CommunityOpensocialCreateSpaceOutput{
		Uri: uri.String(),
	})
}
