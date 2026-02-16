package oauthclient

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type DummyDirectory struct {
	pdsUrl     string
	PrivateKey *atcrypto.PrivateKeyK256
}

func NewDummyDirectory(pdsUrl string) *DummyDirectory {
	privateKey, _ := atcrypto.GeneratePrivateKeyK256()
	return &DummyDirectory{
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
	return &identity.Identity{
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
			"habitat": {
				URL: "https://habitat.network",
			},
		},
	}
}
