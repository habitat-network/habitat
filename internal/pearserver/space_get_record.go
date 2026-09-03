package pearserver

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func (p *PearServer) GetRecord(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatSpaceGetRecordParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space uri")
	if !ok {
		return
	}
	_, ok = p.validator.Request(
		authn.WithMethods(
			authn.ValidatorMethodOAuth,
			authn.ValidatorMethodServiceAuth,
			authn.ValidatorMethodSpaceCredential,
		),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleReader),
	).Validate(w, r)
	if !ok {
		return
	}
	collection, ok := httpx.ParseNSIDInput(ctx, w, params.Collection, "collection")
	if !ok {
		return
	}
	rkey, err := syntax.ParseRecordKey(params.Rkey)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid rkey", err)
		return
	}
	repo, ok := httpx.ParseDIDInput(ctx, w, params.Repo, "repo")
	if !ok {
		return
	}
	rec, err := p.spacesStore.GetRecord(ctx, spaceURI, repo, collection, rkey)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		httpx.WriteRecordNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("get record: %w", err))
		return
	}
	httpx.WriteJSON(r.Context(), w, habitat.NetworkHabitatSpaceGetRecordOutput{
		Uri: habitat_syntax.ConstructSpaceRecordURI(spaceURI, repo, collection, rec.Rkey).
			String(),
		Cid:   rec.Cid.String(),
		Value: rec.Value,
	})
}
