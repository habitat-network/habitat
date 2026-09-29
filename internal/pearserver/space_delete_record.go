package pearserver

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func (p *PearServer) DeleteRecord(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input habitat.NetworkHabitatSpaceDeleteRecordInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	repo, ok := httpx.ParseDIDInput(ctx, w, input.Repo, "repo")
	if !ok {
		return
	}
	if credInfo.Subject != repo {
		httpx.WriteInvalidRequest(ctx, w, "can't write to other repo", nil)
		return
	}
	collection, ok := httpx.ParseNSIDInput(ctx, w, input.Collection, "collection")
	if !ok {
		return
	}
	if habitat_syntax.ReservedCollections.Contains(collection) {
		httpx.WriteInvalidRequest(
			ctx,
			w,
			"relationship collections must be managed by network.habitat.relationship.* lexicons",
			nil,
		)
		return
	}
	if err := p.spacesStore.DeleteRecord(ctx, spaceURI, repo, collection, input.Rkey); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("delete record: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceDeleteRecordOutput{})
}
