package pearserver

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/opensocial"
)

// ListSearchCollections implements network.habitat.search.listCollections.
func (p *PearServer) ListSearchCollections(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var params habitat.NetworkHabitatSearchListCollectionsParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode query params", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, params.Org, "org")
	if !ok {
		return
	}
	if !p.requireAction(ctx, w, org, credInfo.Subject, searchConfigureAction) {
		return
	}
	configured, err := p.searchConfig.List(ctx, org)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list search collections: %w", err))
		return
	}
	collections := nsidStrings(configured)
	defaults := nsidStrings(p.searchConfig.Defaults())
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSearchListCollectionsOutput{
		Collections: collections,
		Defaults:    defaults,
	})
}

// AddSearchCollection implements network.habitat.search.addCollection.
func (p *PearServer) AddSearchCollection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input habitat.NetworkHabitatSearchAddCollectionInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "reading request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	collection, ok := httpx.ParseNSIDInput(ctx, w, input.Collection, "collection")
	if !ok {
		return
	}
	if !p.requireAction(ctx, w, org, credInfo.Subject, searchConfigureAction) {
		return
	}
	if err := p.searchConfig.Add(ctx, org, collection); err != nil {
		httpx.WriteServerError(ctx, w, err)
		return
	}
}

// RemoveSearchCollection implements network.habitat.search.removeCollection.
func (p *PearServer) RemoveSearchCollection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input habitat.NetworkHabitatSearchRemoveCollectionInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "reading request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	collection, ok := httpx.ParseNSIDInput(ctx, w, input.Collection, "collection")
	if !ok {
		return
	}
	if !p.requireAction(ctx, w, org, credInfo.Subject, searchConfigureAction) {
		return
	}
	if err := p.searchConfig.Remove(ctx, org, collection); err != nil {
		httpx.WriteServerError(ctx, w, err)
		return
	}
}

// searchConfigureAction is the action required to configure an org's search.
// Search configuration has no action of its own; it is part of configuring
// the community.
const searchConfigureAction = opensocial.ActionCommunityConfigure

func nsidStrings(nsids []syntax.NSID) []string {
	out := make([]string, len(nsids))
	for i, n := range nsids {
		out[i] = n.String()
	}
	return out
}
