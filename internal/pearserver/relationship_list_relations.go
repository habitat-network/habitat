package pearserver

import (
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// ListRelations implements network.habitat.relationship.listRelations.
func (p *PearServer) ListRelations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatRelationshipListRelationsParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode query params", err)
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space uri")
	if !ok {
		return
	}
	if _, ok = p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(space, habitat_syntax.SpaceRoleReader),
	).Validate(w, r); !ok {
		return
	}
	if params.SubjectType != "" && params.SubjectType != "user" && params.SubjectType != "space" {
		httpx.WriteInvalidRequest(ctx, w, "invalid subjectType", nil)
		return
	}

	views := make([]any, 0)

	if params.SubjectType != "space" {
		userViews, err := p.listUserRelationViews(ctx, space, params)
		if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("list user relations: %w", err))
			return
		}
		views = append(views, userViews...)
	}
	if params.SubjectType != "user" {
		spaceViews, err := p.listSpaceRelationViews(ctx, space, params)
		if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("list space relations: %w", err))
			return
		}
		views = append(views, spaceViews...)
	}

	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatRelationshipListRelationsOutput{Relations: views})
}
