package pearserver

import (
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
)

func (p *PearServer) ListSpaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var params habitat.NetworkHabitatSpaceListSpacesParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode query params", err)
		return
	}
	var filterOwner *syntax.DID
	if params.Did != "" {
		ownerDid, ok := httpx.ParseDIDInput(ctx, w, params.Did, "did")
		if !ok {
			return
		}
		filterOwner = &ownerDid
	}
	var filterType *syntax.NSID
	if params.Type != "" {
		t, ok := httpx.ParseNSIDInput(ctx, w, params.Type, "type filter")
		if !ok {
			return
		}
		filterType = &t
	}
	spaces, err := p.spacesStore.ListSpaces(
		ctx,
		credInfo.Subject,
		filterOwner,
		filterType,
	)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list spaces: %w", err))
		return
	}
	views := make([]habitat.NetworkHabitatSpaceListSpacesSpaceView, len(spaces))
	for i, uri := range spaces {
		views[i] = habitat.NetworkHabitatSpaceListSpacesSpaceView{
			Uri:     uri.String(),
			IsOwner: uri.SpaceOwner() == credInfo.Subject,
		}
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceListSpacesOutput{
		Spaces: views,
	})
}
