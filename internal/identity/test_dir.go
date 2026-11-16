package identity

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
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
// These identities only exist for the passthrough login (see
// login.NewPassthroughProvider). Each publishes a signing key derived from the
// host key (see TestSigningKey) so service auth minted for it can be verified.
// Every other identifier goes to the wrapped directory.
type TestDirectory struct {
	base    identity.Directory
	hostKey atcrypto.PrivateKeyExportable
}

// NewTestDirectory wraps base so ".test" handles resolve locally. hostKey is
// the seed for the identities' signing keys.
func NewTestDirectory(
	base identity.Directory,
	hostKey atcrypto.PrivateKeyExportable,
) identity.Directory {
	return &TestDirectory{base: base, hostKey: hostKey}
}

// IsTestDID reports whether did is a did:web:*.test DID.
func IsTestDID(did syntax.DID) bool {
	_, ok := testDIDHandle(did)
	return ok
}

// TestSigningKey deterministically derives the P-256 signing key of a
// did:web:*.test identity from hostKey, so it needs no storage and is stable
// across restarts. Anyone holding hostKey can sign as any .test identity, which
// is fine because the passthrough login already lets anyone be one.
func TestSigningKey(
	hostKey atcrypto.PrivateKeyExportable,
	did syntax.DID,
) (*atcrypto.PrivateKeyP256, error) {
	for i := range 16 {
		// A uniformly random 32-byte string is a valid P-256 scalar with
		// overwhelming probability; the counter only guards the remainder.
		h := sha256.Sum256(
			fmt.Appendf(nil, "habitat-test-did-signing-key-v1/%d/%s/%x", i, did, hostKey.Bytes()),
		)
		if key, err := atcrypto.ParsePrivateBytesP256(h[:]); err == nil {
			return key, nil
		}
	}
	return nil, errors.New("derive test signing key: no valid scalar")
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

func (d *TestDirectory) identity(
	did syntax.DID,
	handle syntax.Handle,
) (*identity.Identity, error) {
	priv, err := TestSigningKey(d.hostKey, did)
	if err != nil {
		return nil, err
	}
	pub, err := priv.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("test signing public key: %w", err)
	}
	return &identity.Identity{
		DID:         did,
		Handle:      handle,
		AlsoKnownAs: []string{"at://" + string(handle)},
		Services:    map[string]identity.ServiceEndpoint{},
		Keys: map[string]identity.VerificationMethod{
			"atproto": {
				Type:               "Multikey",
				PublicKeyMultibase: pub.Multibase(),
			},
		},
	}, nil
}

// LookupHandle implements identity.Directory.
func (d *TestDirectory) LookupHandle(
	ctx context.Context,
	handle syntax.Handle,
) (*identity.Identity, error) {
	if did, ok := testHandleDID(handle); ok {
		return d.identity(did, handle.Normalize())
	}
	return d.base.LookupHandle(ctx, handle)
}

// LookupDID implements identity.Directory.
func (d *TestDirectory) LookupDID(
	ctx context.Context,
	did syntax.DID,
) (*identity.Identity, error) {
	if handle, ok := testDIDHandle(did); ok {
		return d.identity(did, handle)
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
