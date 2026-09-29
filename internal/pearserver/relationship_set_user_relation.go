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

// SetUserRelation implements network.habitat.relationship.setUserRelation.
func (p *PearServer) SetUserRelation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatRelationshipSetUserRelationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode request body", err)
		return
	}
	subject, ok := httpx.ParseDIDInput(ctx, w, input.Subject, "subject")
	if !ok {
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space")
	if !ok {
		return
	}
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(space, habitat_syntax.SpaceRoleManager),
	).Validate(w, r)
	if !ok {
		return
	}
	role, err := parseSpaceRole(input.Relation)
	if err != nil {
		httpx.WriteError(ctx, w, "InvalidRelation", err.Error(), http.StatusBadRequest)
		return
	}
	isSubjectCurrentlyOwner, err := p.permStore.CheckUserHasSpaceRole(
		ctx,
		subject,
		space,
		habitat_syntax.SpaceRoleOwner,
	)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("check subject is owner: %w", err))
		return
	}
	if !p.authorizeCanWrite(ctx, w, credInfo, isSubjectCurrentlyOwner, space, role) {
		return
	}
	uri, err := p.permStore.SetUserRelation(ctx, subject, space, role)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("add user relation: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w,
		habitat.NetworkHabitatRelationshipSetUserRelationOutput{Uri: uri.String()})
}
