package pearserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
)

// GetProfiles implements network.habitat.opensocial.getProfiles: fetches the
// profiles of members within org. A member with a community.opensocial.memberProfile
// record uses it; a member without one falls back to their public Bluesky
// profile. A requested DID that is neither a member of org nor known to hive
// likewise gets its public Bluesky profile, and is omitted if it has none.
// A hive identity that isn't a member of org is omitted. Callable by any
// member of org.
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
	var external []syntax.DID
	for _, did := range dids {
		roles, err := p.opensocialStore.GetUserRoles(ctx, org, did)
		if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("get user roles: %w", err))
			return
		}
		if len(roles) > 0 {
			members = append(members, did)
			continue
		}
		// Not in the org: only DIDs hive doesn't know are public atproto
		// accounts we can fall back to Bluesky for; hive identities that
		// aren't org members stay hidden.
		if _, err := p.hive.LookupDID(ctx, did); errors.Is(err, identity.ErrDIDNotFound) {
			external = append(external, did)
		} else if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("lookup hive did: %w", err))
			return
		}
	}

	views, err := p.buildProfileViews(ctx, org, members)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("build profile views: %w", err))
		return
	}
	externalViews, err := p.buildExternalProfileViews(ctx, external)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("build external profile views: %w", err))
		return
	}
	views = append(views, externalViews...)

	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatOpensocialGetProfilesOutput{Profiles: views})
}

// buildProfileViews resolves each of members' profile within org: their
// community.opensocial.memberProfile record if they have one, or their
// public Bluesky profile otherwise, plus their org handle.
func (p *PearServer) buildProfileViews(
	ctx context.Context,
	org syntax.DID,
	members []syntax.DID,
) ([]habitat.NetworkHabitatOpensocialGetProfilesProfileView, error) {
	memberProfiles, err := p.opensocialStore.GetMemberProfiles(ctx, org, members)
	if err != nil {
		return nil, fmt.Errorf("get member profiles: %w", err)
	}

	var noProfile []syntax.DID
	for _, did := range members {
		if _, ok := memberProfiles[did]; !ok {
			noProfile = append(noProfile, did)
		}
	}
	bskyProfiles, err := p.fetchBskyProfiles(ctx, noProfile)
	if err != nil {
		return nil, fmt.Errorf("fetch bluesky profiles: %w", err)
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
		if ident, err := p.hive.LookupDID(ctx, did); err == nil {
			view.Handle = ident.Handle.String()
		}
		switch {
		case memberProfiles[did].UpdatedAt != "":
			profile := memberProfiles[did]
			view.DisplayName = profile.DisplayName
			view.Bio = profile.Bio
			view.AvatarUrl = profile.AvatarUrl
		case bskyByDID[did] != nil:
			applyBskyProfile(&view, bskyByDID[did])
		}
		views = append(views, view)
	}
	return views, nil
}

// buildExternalProfileViews returns the public Bluesky profile of each of dids,
// which are neither org members nor known to hive. A DID with no Bluesky
// profile is omitted.
func (p *PearServer) buildExternalProfileViews(
	ctx context.Context,
	dids []syntax.DID,
) ([]habitat.NetworkHabitatOpensocialGetProfilesProfileView, error) {
	bskyProfiles, err := p.fetchBskyProfiles(ctx, dids)
	if err != nil {
		return nil, fmt.Errorf("fetch bluesky profiles: %w", err)
	}
	views := make([]habitat.NetworkHabitatOpensocialGetProfilesProfileView, 0, len(bskyProfiles))
	for _, profile := range bskyProfiles {
		view := habitat.NetworkHabitatOpensocialGetProfilesProfileView{
			Did:    profile.Did,
			Handle: profile.Handle,
		}
		applyBskyProfile(&view, profile)
		views = append(views, view)
	}
	return views, nil
}

// applyBskyProfile copies the display fields of a public Bluesky profile onto view.
func applyBskyProfile(
	view *habitat.NetworkHabitatOpensocialGetProfilesProfileView,
	profile *bsky.ActorDefs_ProfileViewDetailed,
) {
	if profile.DisplayName != nil {
		view.DisplayName = *profile.DisplayName
	}
	if profile.Description != nil {
		view.Bio = *profile.Description
	}
	if profile.Avatar != nil {
		view.AvatarUrl = *profile.Avatar
	}
}

// fetchBskyProfiles fetches public Bluesky profiles via the configured fetcher.
func (p *PearServer) fetchBskyProfiles(
	ctx context.Context,
	dids []syntax.DID,
) ([]*bsky.ActorDefs_ProfileViewDetailed, error) {
	return p.bskyProfiles(ctx, dids)
}
