package pdsclient

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type options struct {
	withHabitatService string
}

type Option func(*options)

func WithHabitatService(habitatURL string) Option {
	return func(o *options) {
		o.withHabitatService = habitatURL
	}
}

type DummyDirectory struct {
	options    *options
	PdsURL     string
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
		PdsURL:     pdsUrl,
		PrivateKey: privateKey,
	}
}

func (d *DummyDirectory) LookupHandle(
	ctx context.Context,
	handle syntax.Handle,
) (*identity.Identity, error) {
	return d.getIdentity(handle, ""), nil
}

func (d *DummyDirectory) LookupDID(
	ctx context.Context,
	did syntax.DID,
) (*identity.Identity, error) {
	return d.getIdentity("", did), nil
}

func (d *DummyDirectory) Lookup(
	ctx context.Context,
	atid syntax.AtIdentifier,
) (*identity.Identity, error) {
	return d.getIdentity(atid.Handle(), atid.DID()), nil
}

func (d *DummyDirectory) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	return fmt.Errorf("unimplemented")
}

func (d *DummyDirectory) getIdentity(handle syntax.Handle, did syntax.DID) *identity.Identity {
	resolvedDID := did
	if resolvedDID == "" {
		resolvedDID = "did:web:example.did.com"
	}
	resolvedHandle := handle
	if resolvedHandle == "" {
		resolvedHandle = "example.handle.com"
	}
	publicKey, _ := d.PrivateKey.PublicKey()
	id := &identity.Identity{
		DID:    resolvedDID,
		Handle: resolvedHandle,
		AlsoKnownAs: []string{
			"at://" + resolvedHandle.String(),
		},
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {
				URL: d.PdsURL,
			},
		},
		Keys: map[string]identity.VerificationMethod{
			"atproto": {
				Type:               "Multikey",
				PublicKeyMultibase: publicKey.Multibase(),
			},
		},
	}
	if d.options.withHabitatService != "" {
		id.Services["habitat"] = identity.ServiceEndpoint{
			URL: d.options.withHabitatService,
		}
	}
	return id
}
