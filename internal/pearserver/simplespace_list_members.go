package pearserver

import (
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// ListMembers implements network.habitat.simplespace.listMembers.
func (p *PearServer) ListMembers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatSimplespaceListMembersParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space uri")
	if !ok {
		return
	}
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleReader),
	).Validate(w, r)
	if !ok {
		return
	}
	dids, err := p.simpleStore.ListMembers(ctx, credInfo.Org.DID(), spaceURI)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list members: %w", err))
		return
	}
	members := make([]habitat.NetworkHabitatSimplespaceListMembersMember, len(dids))
	for i, did := range dids {
		members[i] = habitat.NetworkHabitatSimplespaceListMembersMember{Did: did.String()}

	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSimplespaceListMembersOutput{Members: members})
}
