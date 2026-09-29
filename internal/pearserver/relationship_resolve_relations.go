package pearserver

import (
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// ResolveRelations implements network.habitat.relationship.resolveRelations.
func (p *PearServer) ResolveRelations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatRelationshipResolveRelationsParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode query params", err)
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space")
	if !ok {
		return
	}
	if _, ok = p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(space, habitat_syntax.SpaceRoleReader),
	).Validate(w, r); !ok {
		return
	}
	role, err := parseSpaceRole(params.Relation)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse relation", err)
		return
	}
	dids, err := p.permStore.ListUserSubjects(ctx, space, role)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list subjects: %w", err))
		return
	}
	out := make([]string, len(dids))
	for i, did := range dids {
		out[i] = did.String()
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatRelationshipResolveRelationsOutput{Dids: out})
}
