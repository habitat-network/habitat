package identity

import (
	"context"
	"errors"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// WrappedDirectory resolves identities from a base directory (e.g. the public
// AT Protocol directory) and falls back to a secondary directory (e.g. hive)
// for identities the base directory doesn't know about, such as org members
// served internally and not publicly resolvable.
type WrappedDirectory struct {
	base     identity.Directory
	fallback identity.Directory
}

func NewWrappedDirectory(base, fallback identity.Directory) identity.Directory {
	return &WrappedDirectory{base: base, fallback: fallback}
}

// LookupHandle implements identity.Directory.
func (d *WrappedDirectory) LookupHandle(
	ctx context.Context,
	handle syntax.Handle,
) (*identity.Identity, error) {
	id, err := d.base.LookupHandle(ctx, handle)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, identity.ErrHandleResolutionFailed) &&
		!errors.Is(err, identity.ErrHandleNotFound) {
		return nil, err
	}
	return d.fallback.LookupHandle(ctx, handle)
}

// LookupDID implements identity.Directory.
func (d *WrappedDirectory) LookupDID(
	ctx context.Context,
	did syntax.DID,
) (*identity.Identity, error) {
	id, err := d.base.LookupDID(ctx, did)
	if err == nil {
		return id, nil
	}

	if !errors.Is(err, identity.ErrDIDNotFound) &&
		!errors.Is(err, identity.ErrDIDResolutionFailed) {
		return nil, err
	}
	return d.fallback.LookupDID(ctx, did)
}

// Lookup implements identity.Directory.
func (d *WrappedDirectory) Lookup(
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
func (d *WrappedDirectory) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	if err := d.base.Purge(ctx, atid); err != nil {
		return err
	}
	return d.fallback.Purge(ctx, atid)
}
