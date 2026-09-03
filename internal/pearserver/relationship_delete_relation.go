package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/perms"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// DeleteRelation implements network.habitat.relationship.deleteRelation.
func (p *PearServer) DeleteRelation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatRelationshipDeleteRelationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode request body", err)
		return
	}
	uri, err := habitat_syntax.ParseSpaceRecordURI(input.Uri)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse uri", err)
		return
	}
	if _, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(uri.SpaceURI(), habitat_syntax.SpaceRoleManager),
	).Validate(w, r); !ok {
		return
	}
	err = p.permStore.DeleteRelation(ctx, uri)
	if errors.Is(err, perms.ErrRelationNotFound) {
		slog.WarnContext(ctx, "relation not found", "err", err)
		httpx.WriteError(ctx, w, "RelationNotFound", "", http.StatusNotFound)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("delete relation: %w", err))
		return
	}
}
