package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"
)

// UpdateOpensocialSpace implements community.opensocial.updateSpace: replaces
// the roles that may read a space by rewriting its community.opensocial.access
// record. Requires service-auth, since callers reach this via Atproto-Proxy on
// the caller's own session rather than a dedicated org session — see the chalk
// org-support design doc.
func (p *PearServer) UpdateOpensocialSpace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.CommunityOpensocialUpdateSpaceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space")
	if !ok {
		return
	}
	if !p.requireMember(ctx, w, spaceURI.SpaceOwner(), credInfo.Subject) {
		return
	}
	if err := p.opensocialStore.UpdateSpace(ctx, spaceURI, input.Roles); err != nil {
		if errors.Is(err, spaces.ErrSpaceNotFound) {
			httpx.WriteSpaceNotFound(ctx, w, err)
			return
		}
		httpx.WriteServerError(ctx, w, fmt.Errorf("update space: %w", err))
		return
	}
}
