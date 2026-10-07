package pearserver

import (
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// ListInheritingSpaces implements network.habitat.relationship.listInheritingSpaces.
func (p *PearServer) ListInheritingSpaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatRelationshipListInheritingSpacesParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode query params", err)
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space")
	if !ok {
		return
	}
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(space, habitat_syntax.SpaceRoleReader),
	).Validate(w, r)
	if !ok {
		return
	}
	role := habitat_syntax.SpaceRoleReader
	if params.Relation != "" {
		var err error
		role, err = parseSpaceRole(params.Relation)
		if err != nil {
			httpx.WriteInvalidRequest(ctx, w, "failed to parse relation", err)
			return
		}
	}
	inheriting, err := p.permStore.ListInheritingSpaces(ctx, space, role)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list inheriting spaces: %w", err))
		return
	}
	// Only return spaces the caller is allowed to read.
	out := make([]string, 0, len(inheriting))
	for _, s := range inheriting {
		readable, err := p.permStore.CheckUserHasSpaceRole(
			ctx,
			credInfo.Subject,
			s,
			habitat_syntax.SpaceRoleReader,
		)
		if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("check read permission: %w", err))
			return
		}
		if readable {
			out = append(out, s.String())
		}
	}
	httpx.WriteJSON(
		ctx,
		w,
		habitat.NetworkHabitatRelationshipListInheritingSpacesOutput{Spaces: out},
	)
}
