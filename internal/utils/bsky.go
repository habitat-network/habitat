package utils

import (
	"context"

	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xslices"
	"github.com/habitat-network/habitat/internal/httpx"
)

const (
	host = "https://public.api.bsky.app"
)

// maxBskyProfilesPerCall is app.bsky.actor.getProfiles' own limit on the
// number of actors accepted in a single request.
const maxBskyProfilesPerCall = 25

// FetchProfiles batch-fetches public Bluesky profiles for dids from the
// public appview, chunking the request to respect its per-call actor limit.
// dids with no Bluesky account are simply absent from the result.
func FetchProfiles(
	ctx context.Context,
	dids []syntax.DID,
) ([]*bsky.ActorDefs_ProfileViewDetailed, error) {
	if len(dids) == 0 {
		return nil, nil
	}
	client := &atclient.APIClient{Host: host, Client: httpx.NewClient()}
	var profiles []*bsky.ActorDefs_ProfileViewDetailed
	for chunkStart := 0; chunkStart < len(dids); chunkStart += maxBskyProfilesPerCall {
		chunkEnd := min(chunkStart+maxBskyProfilesPerCall, len(dids))
		actors := xslices.Map(
			dids[chunkStart:chunkEnd],
			func(d syntax.DID) string { return d.String() },
		)
		output, err := bsky.ActorGetProfiles(ctx, client, actors)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, output.Profiles...)
	}
	return profiles, nil
}
