package handles

import (
	"context"
	"errors"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
)

var ErrHandleExists = errors.New("handle already exists")

type Handles interface {
	MintHandle(ctx context.Context, handle string, did syntax.DID) error
	LookupHandle(ctx context.Context, handle syntax.Handle) (*identity.Identity, error)
}

type handlesImpl struct {
	store *store
}

func New(db *gorm.DB) (Handles, error) {
	s, err := newStore(db)
	if err != nil {
		return nil, err
	}
	return &handlesImpl{store: s}, nil
}

var _ Handles = &handlesImpl{}

func (h *handlesImpl) MintHandle(ctx context.Context, handle string, did syntax.DID) error {
	return h.store.create(ctx, handle, did.String())
}

func (h *handlesImpl) LookupHandle(ctx context.Context, handle syntax.Handle) (*identity.Identity, error) {
	didStr, err := h.store.get(ctx, handle.String())
	if err != nil {
		return nil, identity.ErrHandleNotFound
	}
	did, err := syntax.ParseDID(didStr)
	if err != nil {
		return nil, identity.ErrHandleNotFound
	}
	return &identity.Identity{
		DID:    did,
		Handle: handle,
	}, nil
}
