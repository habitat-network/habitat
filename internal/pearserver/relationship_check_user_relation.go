package pearserver

import (
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// CheckUserRelation implements network.habitat.relationship.checkUserRelation.
func (p *PearServer) CheckUserRelation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatRelationshipCheckUserRelationParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode query params", err)
		return
	}
	subject, ok := httpx.ParseDIDInput(ctx, w, params.Subject, "subject")
	if !ok {
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
	allowed, err := p.permStore.CheckUserHasSpaceRole(ctx, subject, space, role)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("check user relation: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w,
		habitat.NetworkHabitatRelationshipCheckUserRelationOutput{Allowed: allowed})
}
