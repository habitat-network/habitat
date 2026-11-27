package pearserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/utils"
)

// GetProfiles implements network.habitat.opensocial.getProfiles: fetches the
// profiles of members within org. A member with a network.habitat.opensocial.memberProfile
// record uses it; a member without one falls back to their public Bluesky
// profile. Callable by any member of org; requested DIDs that aren't members
// of org are omitted from the response.
func (p *PearServer) GetProfiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatOpensocialGetProfilesParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode query params", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, params.Org, "org")
	if !ok {
		return
	}
	if !p.requireMember(ctx, w, org, credInfo.Subject) {
		return
	}

	dids := make([]syntax.DID, len(params.Dids))
	for i, didStr := range params.Dids {
		did, ok := httpx.ParseDIDInput(ctx, w, didStr, "dids")
		if !ok {
			return
		}
		dids[i] = did
	}

	members := make([]syntax.DID, 0, len(dids))
	for _, did := range dids {
		roles, err := p.opensocialStore.GetUserRoles(ctx, org, did)
		if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("get user roles: %w", err))
			return
		}
		if len(roles) > 0 {
			members = append(members, did)
		}
	}

	views, err := p.buildProfileViews(ctx, org, members)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("build profile views: %w", err))
		return
	}

	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatOpensocialGetProfilesOutput{Profiles: views})
}

// buildProfileViews resolves each of members' profile within org: their
// network.habitat.opensocial.memberProfile record if they have one, or their
// public Bluesky profile otherwise, plus their handle from identity resolution.
func (p *PearServer) buildProfileViews(
	ctx context.Context,
	org syntax.DID,
	members []syntax.DID,
) ([]habitat.NetworkHabitatOpensocialGetProfilesProfileView, error) {
	memberProfiles, err := p.opensocialStore.GetMemberProfiles(ctx, org, members)
	if err != nil {
		return nil, fmt.Errorf("get member profiles: %w", err)
	}

	// Members without a member profile fall back to their public Bluesky
	// profile. Bluesky has no placeholder for DIDs without an account, so
	// those are simply absent from the result.
	var noProfile []syntax.DID
	for _, did := range members {
		if memberProfiles[did].UpdatedAt == "" {
			noProfile = append(noProfile, did)
		}
	}
	// The Bluesky fallback only enriches the view, so a failed lookup (Bluesky
	// down or unreachable, an identity it rejects) leaves those members with
	// just their handle instead of failing the whole request.
	bskyProfiles, err := utils.FetchProfiles(ctx, noProfile)
	if err != nil {
		slog.WarnContext(ctx, "fetch bluesky profiles", "err", err)
		bskyProfiles = nil
	}
	bskyByDID := make(map[syntax.DID]*bsky.ActorDefs_ProfileViewDetailed, len(bskyProfiles))
	for _, profile := range bskyProfiles {
		bskyByDID[syntax.DID(profile.Did)] = profile
	}

	views := make([]habitat.NetworkHabitatOpensocialGetProfilesProfileView, 0, len(members))
	for _, did := range members {
		view := habitat.NetworkHabitatOpensocialGetProfilesProfileView{
			Did:    did.String(),
			Handle: did.String(),
		}
		// Resolve the identity through the general directory (hive, PLC,
		// did:web, ...) for the handle. If it can't be resolved the handle
		// stays the DID.
		if ident, err := p.dir.LookupDID(ctx, did); err == nil && !ident.Handle.IsInvalidHandle() {
			view.Handle = ident.Handle.String()
		}
		switch {
		case memberProfiles[did].UpdatedAt != "":
			profile := memberProfiles[did]
			view.DisplayName = profile.DisplayName
			view.Bio = profile.Bio
			view.AvatarUrl = profile.AvatarUrl
		case bskyByDID[did] != nil:
			bskyProfile := bskyByDID[did]
			if bskyProfile.DisplayName != nil {
				view.DisplayName = *bskyProfile.DisplayName
			}
			if bskyProfile.Description != nil {
				view.Bio = *bskyProfile.Description
			}
			if bskyProfile.Avatar != nil {
				view.AvatarUrl = *bskyProfile.Avatar
			}
		}
		views = append(views, view)
	}
	return views, nil
}
