package pdsclient

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type options struct {
	withHabitatService bool
}

type Option func(*options)

func WithHabitatService() Option {
	return func(o *options) {
		o.withHabitatService = true
	}
}

type DummyDirectory struct {
	options    *options
	pdsUrl     string
	PrivateKey *atcrypto.PrivateKeyK256
}

func NewDummyDirectory(pdsUrl string, opts ...Option) *DummyDirectory {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}
	privateKey, _ := atcrypto.GeneratePrivateKeyK256()
	return &DummyDirectory{
		options:    o,
		pdsUrl:     pdsUrl,
		PrivateKey: privateKey,
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
	return d.getIdentity(did.String()), nil
}

func (d *DummyDirectory) Lookup(
	ctx context.Context,
	atid syntax.AtIdentifier,
) (*identity.Identity, error) {
	return d.getIdentity(atid.String()), nil
}

func (d *DummyDirectory) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	return fmt.Errorf("unimplemented")
}

func (d *DummyDirectory) getIdentity(did string) *identity.Identity {
	publicKey, _ := d.PrivateKey.PublicKey()
	id := &identity.Identity{
		DID: syntax.DID(did),
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {
				URL: d.pdsUrl,
			},
		},
		Keys: map[string]identity.VerificationMethod{
			"atproto": {
				Type:               "Multikey",
				PublicKeyMultibase: publicKey.Multibase(),
			},
		},
	}
	if d.options.withHabitatService {
		id.Services["habitat"] = identity.ServiceEndpoint{
			URL: "https://habitat.network",
		}
	}
	return id
}
