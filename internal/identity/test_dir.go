package identity

import (
	"context"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

const (
	// testHandleSuffix is the reserved TLD (RFC 2606) of handles that never
	// resolve publicly; TestDirectory resolves them locally.
	testHandleSuffix = ".test"
	didWebPrefix     = "did:web:"
)

// TestDirectory resolves handles ending in ".test" to did:web:<handle> and
// did:web:<name>.test DIDs back to the handle, without any network lookup.
// The identities it returns carry no keys or services, since .test identities
// only exist for the passthrough login (see login.NewPassthroughProvider).
// Every other identifier goes to the wrapped directory.
type TestDirectory struct {
	base identity.Directory
}

// NewTestDirectory wraps base so ".test" handles resolve locally.
func NewTestDirectory(base identity.Directory) identity.Directory {
	return &TestDirectory{base: base}
}

// testHandleDID returns the did:web DID a .test handle resolves to.
func testHandleDID(handle syntax.Handle) (syntax.DID, bool) {
	h := handle.Normalize()
	if !strings.HasSuffix(string(h), testHandleSuffix) {
		return "", false
	}
	return syntax.DID(didWebPrefix + string(h)), true
}

// testDIDHandle returns the .test handle a did:web DID resolves to.
func testDIDHandle(did syntax.DID) (syntax.Handle, bool) {
	name, ok := strings.CutPrefix(string(did), didWebPrefix)
	if !ok || !strings.HasSuffix(name, testHandleSuffix) {
		return "", false
	}
	handle, err := syntax.ParseHandle(name)
	if err != nil {
		return "", false
	}
	return handle, true
}

func testIdentity(did syntax.DID, handle syntax.Handle) *identity.Identity {
	return &identity.Identity{
		DID:         did,
		Handle:      handle,
		AlsoKnownAs: []string{"at://" + string(handle)},
		Services:    map[string]identity.ServiceEndpoint{},
		Keys:        map[string]identity.VerificationMethod{},
	}
}

// LookupHandle implements identity.Directory.
func (d *TestDirectory) LookupHandle(
	ctx context.Context,
	handle syntax.Handle,
) (*identity.Identity, error) {
	if did, ok := testHandleDID(handle); ok {
		return testIdentity(did, handle.Normalize()), nil
	}
	return d.base.LookupHandle(ctx, handle)
}

// LookupDID implements identity.Directory.
func (d *TestDirectory) LookupDID(
	ctx context.Context,
	did syntax.DID,
) (*identity.Identity, error) {
	if handle, ok := testDIDHandle(did); ok {
		return testIdentity(did, handle), nil
	}
	return d.base.LookupDID(ctx, did)
}

// Lookup implements identity.Directory.
func (d *TestDirectory) Lookup(
	ctx context.Context,
	atid syntax.AtIdentifier,
) (*identity.Identity, error) {
	if atid.IsDID() {
		did, err := atid.AsDID()
		if err != nil {
			return nil, err
		}
		return d.LookupDID(ctx, did)
	}
	handle, err := atid.AsHandle()
	if err != nil {
		return nil, err
	}
	return d.LookupHandle(ctx, handle)
}

// Purge implements identity.Directory.
func (d *TestDirectory) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	return d.base.Purge(ctx, atid)
}
