package pearserver

import (
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/comatproto"
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
	// com.atproto.space.listSpaces names the authority filter spaceDid;
	// network.habitat.space.listSpaces names it did.
	var owner, spaceType string
	if httpx.IsComAtprotoRequest(r) {
		var params comatproto.ComAtprotoSpaceListSpacesParams
		if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
			httpx.WriteInvalidRequest(ctx, w, "decode query params", err)
			return
		}
		owner, spaceType = params.SpaceDid, params.SpaceType
	} else {
		var params habitat.NetworkHabitatSpaceListSpacesParams
		if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
			httpx.WriteInvalidRequest(ctx, w, "decode query params", err)
			return
		}
		owner, spaceType = params.Did, params.SpaceType
	}
	var filterOwner *syntax.DID
	if owner != "" {
		ownerDid, ok := httpx.ParseDIDInput(ctx, w, owner, "spaceDid")
		if !ok {
			return
		}
		filterOwner = &ownerDid
	}
	var filterType *syntax.NSID
	if spaceType != "" {
		t, ok := httpx.ParseNSIDInput(ctx, w, spaceType, "type filter")
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
	if httpx.IsComAtprotoRequest(r) {
		views := make([]comatproto.ComAtprotoSpaceListSpacesSpaceView, len(spaces))
		for i, uri := range spaces {
			views[i] = comatproto.ComAtprotoSpaceListSpacesSpaceView{Space: uri.AtSpace().String()}
		}
		httpx.WriteJSON(ctx, w, comatproto.ComAtprotoSpaceListSpacesOutput{Spaces: views})
		return
	}
	views := make([]habitat.NetworkHabitatSpaceListSpacesSpaceView, len(spaces))
	for i, uri := range spaces {
		views[i] = habitat.NetworkHabitatSpaceListSpacesSpaceView{
			Uri: uri.AtSpace().String(),
		}
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceListSpacesOutput{
		Spaces: views,
	})
}
