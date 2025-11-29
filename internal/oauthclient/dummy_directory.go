package oauthclient

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type DummyDirectory struct {
	URL string
}

func NewDummyDirectory(url string) *DummyDirectory {
	return &DummyDirectory{
		URL: url,
	}
}

func (d *DummyDirectory) LookupHandle(
	ctx context.Context,
	handle syntax.Handle,
) (*identity.Identity, error) {
	return nil, fmt.Errorf("unimplemented")
}

func (d *DummyDirectory) LookupDID(
	ctx context.Context,
	did syntax.DID,
) (*identity.Identity, error) {
	return nil, fmt.Errorf("unimplemented")
}

func (d *DummyDirectory) Lookup(
	ctx context.Context,
	atid syntax.AtIdentifier,
) (*identity.Identity, error) {
	did := atid.String()
	return &identity.Identity{
		DID: syntax.DID(did),
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {
				URL: d.URL,
			},
		},
	}, nil
}

func (d *DummyDirectory) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	return fmt.Errorf("unimplemented")
}
