package handles

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestHandles(t *testing.T) (Handles, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	h, err := New(db)
	require.NoError(t, err)
	return h, db
}

func TestMintAndLookupHandle(t *testing.T) {
	ctx := context.Background()
	h, _ := newTestHandles(t)

	did := syntax.DID("did:web:abc123.example.com")
	err := h.MintHandle(ctx, "alice.example.com", did)
	require.NoError(t, err)

	handle, err := syntax.ParseHandle("alice.example.com")
	require.NoError(t, err)
	ident, err := h.LookupHandle(ctx, handle)
	require.NoError(t, err)
	require.Equal(t, did, ident.DID)
	require.Equal(t, handle, ident.Handle)
}

func TestMintHandle_Duplicate(t *testing.T) {
	ctx := context.Background()
	h, _ := newTestHandles(t)

	did := syntax.DID("did:web:abc123.example.com")
	err := h.MintHandle(ctx, "alice.example.com", did)
	require.NoError(t, err)

	err = h.MintHandle(ctx, "alice.example.com", syntax.DID("did:web:def456.example.com"))
	require.ErrorIs(t, err, ErrHandleExists)
}

func TestLookupHandle_NotFound(t *testing.T) {
	ctx := context.Background()
	h, _ := newTestHandles(t)

	handle, err := syntax.ParseHandle("nobody.example.com")
	require.NoError(t, err)
	_, err = h.LookupHandle(ctx, handle)
	require.ErrorIs(t, err, identity.ErrHandleNotFound)
}
