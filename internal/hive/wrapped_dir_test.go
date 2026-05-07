package hive

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

func TestWrappedDirLookupHandleHiveHosted(t *testing.T) {
	h, db := newTestHive(t, "example.com", "pear.example.com")
	mintAndPersist(t, h, db, "alice")

	fallback := identity.NewMockDirectory()
	dir := NewWrappedDirectory(h, fallback)

	ident, err := dir.LookupHandle(context.Background(), syntax.Handle("alice.example.com"))
	require.NoError(t, err)
	require.Equal(t, syntax.Handle("alice.example.com"), ident.Handle)
}

func TestWrappedDirLookupHandleFallback(t *testing.T) {
	h, _ := newTestHive(t, "example.com", "pear.example.com")

	fallback := identity.NewMockDirectory()
	external := identity.Identity{
		DID:    syntax.DID("did:plc:abcdef1234567890abcdefgh"),
		Handle: syntax.Handle("bob.other.com"),
	}
	fallback.Insert(external)

	dir := NewWrappedDirectory(h, fallback)

	ident, err := dir.LookupHandle(context.Background(), syntax.Handle("bob.other.com"))
	require.NoError(t, err)
	require.Equal(t, external.Handle, ident.Handle)
	require.Equal(t, external.DID, ident.DID)
}

func TestWrappedDirLookupDIDHiveHosted(t *testing.T) {
	h, db := newTestHive(t, "example.com", "pear.example.com")
	mintAndPersist(t, h, db, "alice")

	minted, err := h.LookupHandle(context.Background(), syntax.Handle("alice.example.com"))
	require.NoError(t, err)

	fallback := identity.NewMockDirectory()
	dir := NewWrappedDirectory(h, fallback)

	ident, err := dir.LookupDID(context.Background(), minted.DID)
	require.NoError(t, err)
	require.Equal(t, minted.DID, ident.DID)
	require.Equal(t, minted.Handle, ident.Handle)
}

func TestWrappedDirLookupDIDFallback(t *testing.T) {
	h, _ := newTestHive(t, "example.com", "pear.example.com")

	fallback := identity.NewMockDirectory()
	external := identity.Identity{
		DID:    syntax.DID("did:plc:abcdef1234567890abcdefgh"),
		Handle: syntax.Handle("bob.other.com"),
	}
	fallback.Insert(external)

	dir := NewWrappedDirectory(h, fallback)

	ident, err := dir.LookupDID(context.Background(), external.DID)
	require.NoError(t, err)
	require.Equal(t, external.DID, ident.DID)
}
