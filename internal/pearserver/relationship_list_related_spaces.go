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

// ListRelatedSpaces implements network.habitat.relationship.listRelatedSpaces.
func (p *PearServer) ListRelatedSpaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var params habitat.NetworkHabitatRelationshipListRelatedSpacesParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode query params", err)
		return
	}
	did, ok := httpx.ParseDIDInput(ctx, w, params.Did, "did")
	if !ok {
		return
	}
	role, err := parseSpaceRole(params.Relation)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse relation", err)
		return
	}
	var filterType *syntax.NSID
	if params.Type != "" {
		t, ok := httpx.ParseNSIDInput(ctx, w, params.Type, "type filter")
		if !ok {
			return
		}
		filterType = &t
	}
	spaceURIs, err := p.permStore.ListObjects(ctx, did, role, filterType)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list objects: %w", err))
		return
	}
	// Only return spaces the caller is allowed to read.
	out := make([]string, 0, len(spaceURIs))
	for _, space := range spaceURIs {
		readable, err := p.permStore.CheckUserHasSpaceRole(
			ctx,
			credInfo.Subject,
			space,
			habitat_syntax.SpaceRoleReader,
		)
		if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("check read permission: %w", err))
			return
		}
		if readable {
			out = append(out, space.String())
		}
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatRelationshipListRelatedSpacesOutput{Spaces: out})
}
