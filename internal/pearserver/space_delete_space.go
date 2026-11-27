package pearserver

import (
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

// DeleteSpace implements com.atproto.space.deleteSpace.
func (p *PearServer) DeleteSpace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input comatproto.ComAtprotoSpaceDeleteSpaceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleOwner),
	).Validate(w, r)
	if !ok {
		return
	}
	group, ok := p.isGroup(ctx, w, spaceURI.SpaceOwner())
	if !ok {
		return
	}
	if group {
		caller, ok := p.callerFor(ctx, w, credInfo, spaceURI.SpaceOwner())
		if !ok {
			return
		}
		if !caller.Scopes.Covers("space:*?manage=delete") {
			httpx.WriteUnauthorized(ctx, w, "the session may not delete spaces", nil)
			return
		}
		writeGroupSpaceError(ctx, w, p.opensocialStore.DeleteGroupSpace(
			ctx, caller.Group, caller.Actor, spaceURI,
		))
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
