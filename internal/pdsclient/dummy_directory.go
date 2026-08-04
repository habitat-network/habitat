package pdsclient

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/did"
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

func (d *DummyDirectory) getIdentity(handle syntax.Handle, reqDID syntax.DID) *identity.Identity {
	resolvedDID := reqDID
	if resolvedDID == "" {
		resolvedDID = "did:web:example.did.com"
	}
	resolvedHandle := handle
	if resolvedHandle == "" {
		resolvedHandle = "example.handle.com"
	}
	publicKey, _ := d.PrivateKey.PublicKey()
	b := did.New(resolvedDID).
		AlsoKnownAs("at://" + resolvedHandle.String()).
		Atproto(publicKey.Multibase()).
		ATProtoPDS(d.pdsUrl)
	if d.options.withHabitatService != "" {
		b.Habitat(d.options.withHabitatService)
	}
	doc := b.Build()
	// ParseIdentity sets Handle to handle.invalid; overwrite with the real one.
	ident := identity.ParseIdentity(&doc)
	ident.Handle = resolvedHandle
	return &ident
}
