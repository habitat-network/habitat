package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// DeleteSpace implements network.habitat.simplespace.deleteSpace.
func (p *PearServer) DeleteSpace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSimplespaceDeleteSpaceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	_, ok = p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleOwner),
	).Validate(w, r)
	if !ok {
		return
	}
	err := p.simpleStore.DeleteSpace(ctx, spaceURI)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(r.Context(), w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("delete space: %w", err))
		return
	}
}
