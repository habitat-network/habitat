package oauthclient

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type DummyDirectory struct {
	pdsUrl string
}

func NewDummyDirectory(pdsUrl string) *DummyDirectory {
	return &DummyDirectory{
		pdsUrl: pdsUrl,
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
	return getIdentity(d.pdsUrl, did.String()), nil
}

func (d *DummyDirectory) Lookup(
	ctx context.Context,
	atid syntax.AtIdentifier,
) (*identity.Identity, error) {
	return getIdentity(d.pdsUrl, atid.String()), nil
}

func (d *DummyDirectory) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	return fmt.Errorf("unimplemented")
}

func getIdentity(pdsUrl string, did string) *identity.Identity {
	return &identity.Identity{
		DID: syntax.DID(did),
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {
				URL: pdsUrl,
			},
			"habitat": {
				URL: "https://habitat.network",
			},
		},
	}
}
