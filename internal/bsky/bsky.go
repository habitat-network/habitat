package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/bradenaw/juniper/xslices"
)

const (
	host = "https://public.api.bsky.app"
)

func FetchFollowers(ctx context.Context, did syntax.DID) ([]syntax.DID, error) {
	client := &xrpc.Client{
		Host: host,
	}

	output, err := bsky.GraphGetFollowers(ctx, client, did.String(), "", 0)
	if err != nil {
		return nil, err
	}

	followers := xslices.Map(output.Followers, func(a *bsky.ActorDefs_ProfileView) syntax.DID {
		return syntax.DID(a.Did)
	})

	return followers, nil
}
