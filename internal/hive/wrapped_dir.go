package hive

import (
	"context"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type wrappedDir struct {
	fallback identity.Directory
	hive     Hive
}

// hasBaseDomainSuffix returns true if s is of the form "<prefix>.<baseDomain>".
func hasBaseDomainSuffix(s, baseDomain string) bool {
	_, after, ok := strings.Cut(s, ".")
	return ok && after == baseDomain
}

func (w *wrappedDir) didInHive(did syntax.DID) bool {
	content := strings.TrimPrefix(did.String(), "did:web:")
	return hasBaseDomainSuffix(content, w.hive.BaseDomain())
}

func (w *wrappedDir) handleInHive(handle syntax.Handle) bool {
	return hasBaseDomainSuffix(handle.String(), w.hive.BaseDomain())
}

func (w *wrappedDir) directoryFor(atid syntax.AtIdentifier) (identity.Directory, error) {
	if atid.IsDID() {
		did, err := atid.AsDID()
		if err != nil {
			return nil, err
		}
		if w.didInHive(did) {
			return w.hive, nil
		}
		return w.fallback, nil
	}
	handle, err := atid.AsHandle()
	if err != nil {
		return nil, err
	}
	if w.handleInHive(handle) {
		return w.hive, nil
	}
	return w.fallback, nil
}

// Lookup implements [identity.Directory].
func (w *wrappedDir) Lookup(ctx context.Context, atid syntax.AtIdentifier) (*identity.Identity, error) {
	dir, err := w.directoryFor(atid)
	if err != nil {
		return nil, err
	}
	return dir.Lookup(ctx, atid)
}

// LookupDID implements [identity.Directory].
func (w *wrappedDir) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	if w.didInHive(did) {
		return w.hive.LookupDID(ctx, did)
	}
	return w.fallback.LookupDID(ctx, did)
}

// LookupHandle implements [identity.Directory].
func (w *wrappedDir) LookupHandle(ctx context.Context, handle syntax.Handle) (*identity.Identity, error) {
	if w.handleInHive(handle) {
		return w.hive.LookupHandle(ctx, handle)
	}
	return w.fallback.LookupHandle(ctx, handle)
}

// Purge implements [identity.Directory].
func (w *wrappedDir) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	dir, err := w.directoryFor(atid)
	if err != nil {
		return err
	}
	return dir.Purge(ctx, atid)
}

var _ identity.Directory = &wrappedDir{}

func NewWrappedDirectory(hive Hive, fallback identity.Directory) identity.Directory {
	return &wrappedDir{
		fallback: fallback,
		hive:     hive,
	}
}
