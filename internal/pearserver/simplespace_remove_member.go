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

// RemoveMember implements network.habitat.simplespace.removeMember.
func (p *PearServer) RemoveMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSimplespaceRemoveMemberInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	_, ok = p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleManager),
	).Validate(w, r)
	if !ok {
		return
	}
	memberDID, ok := httpx.ParseDIDInput(ctx, w, input.Did, "did")
	if !ok {
		return
	}
	err := p.simpleStore.RemoveMember(r.Context(), spaceURI, memberDID)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if errors.Is(err, spaces.ErrNotAMember) {
		return
	} else if errors.Is(err, simplespace.ErrCannotRemoveOrg) {
		httpx.WriteInvalidRequest(ctx, w, "cannot remove org", err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("remove member: %w", err))
		return
	}
}
