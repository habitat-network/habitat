package hive

import (
	"context"
	"errors"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type wrappedDir struct {
	fallback identity.Directory
	hive     Hive
}

// Lookup implements [identity.Directory].
func (w *wrappedDir) Lookup(ctx context.Context, atid syntax.AtIdentifier) (*identity.Identity, error) {
	id, err := w.hive.Lookup(ctx, atid)
	if err == nil {
		return id, nil
	}
	return w.fallback.Lookup(ctx, atid)
}

// LookupDID implements [identity.Directory].
func (w *wrappedDir) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	id, err := w.hive.LookupDID(ctx, did)
	if err == nil {
		return id, nil
	}
	return w.fallback.LookupDID(ctx, did)
}

// LookupHandle implements [identity.Directory].
func (w *wrappedDir) LookupHandle(ctx context.Context, handle syntax.Handle) (*identity.Identity, error) {
	id, err := w.hive.LookupHandle(ctx, handle)
	if err == nil {
		return id, nil
	}
	return w.fallback.LookupHandle(ctx, handle)
}

// Purge implements [identity.Directory].
func (w *wrappedDir) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	return errors.Join(w.hive.Purge(ctx, atid), w.fallback.Purge(ctx, atid))
}

var _ identity.Directory = &wrappedDir{}

func NewWrappedDirectory(hive Hive, fallback identity.Directory) identity.Directory {
	return &wrappedDir{
		fallback: fallback,
		hive:     hive,
	}
}
