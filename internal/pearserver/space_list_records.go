package pearserver

import (
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func (p *PearServer) ListRecords(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatSpaceListRecordsParams
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
	var filterCollection *syntax.NSID
	if params.Collection != "" {
		c, ok := httpx.ParseNSIDInput(ctx, w, params.Collection, "collection filter")
		if !ok {
			return
		}
		filterCollection = &c
	}
	repo, ok := httpx.ParseDIDInput(ctx, w, params.Repo, "repo")
	if !ok {
		return
	}
	records, err := p.spacesStore.ListRecords(r.Context(), spaceURI, repo, filterCollection)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list records: %w", err))
		return
	}
	recViews := make([]habitat.NetworkHabitatSpaceListRecordsRecord, len(records))
	for i, rec := range records {
		recViews[i] = habitat.NetworkHabitatSpaceListRecordsRecord{
			Collection: rec.Collection.String(),
			Rkey:       rec.Rkey.String(),
			Cid:        rec.Cid.String(),
		}
		if !params.ExcludeValues {
			recViews[i].Value = rec.Value
		}
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceListRecordsOutput{Records: recViews})
}
