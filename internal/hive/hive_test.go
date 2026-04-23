package hive

import (
	"context"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestHive(t *testing.T, domain, memberNamespace string) Hive {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	h, err := NewHive(domain, memberNamespace, db)
	require.NoError(t, err)
	return h
}

func TestMintIdentity(t *testing.T) {
	h := newTestHive(t, "example.com", "")
	require.NoError(t, h.MintIdentity("alice"))
}

func TestMintIdentity_InvalidHandle(t *testing.T) {
	h := newTestHive(t, "example.com", "")
	require.ErrorIs(t, h.MintIdentity("alice!invalid"), identity.ErrInvalidHandle)
}

func TestMintIdentity_Duplicate(t *testing.T) {
	h := newTestHive(t, "example.com", "")

	require.NoError(t, h.MintIdentity("alice"))
	require.ErrorIs(t, h.MintIdentity("alice"), ErrNotCreated)
}

func TestLookupHandle(t *testing.T) {
	h := newTestHive(t, "example.com", "")

	require.NoError(t, h.MintIdentity("alice"))

	ident, err := h.LookupHandle(context.Background(), syntax.Handle("alice.example.com"))
	require.NoError(t, err)
	require.Equal(t, syntax.Handle("alice.example.com"), ident.Handle)
	require.True(t, strings.HasPrefix(ident.DID.String(), "did:web:"))
}

func TestLookupHandle_NotFound(t *testing.T) {
	h := newTestHive(t, "example.com", "")

	_, err := h.LookupHandle(context.Background(), syntax.Handle("nobody.example.com"))
	require.ErrorIs(t, err, identity.ErrHandleNotFound)
}

func TestLookupHandle_WrongDomain(t *testing.T) {
	h := newTestHive(t, "example.com", "")

	require.NoError(t, h.MintIdentity("alice"))

	_, err := h.LookupHandle(context.Background(), syntax.Handle("alice.other.com"))
	require.ErrorIs(t, err, identity.ErrHandleNotFound)
}

func TestLookupDID(t *testing.T) {
	h := newTestHive(t, "example.com", "")

	require.NoError(t, h.MintIdentity("alice"))

	ident, err := h.LookupHandle(context.Background(), syntax.Handle("alice.example.com"))
	require.NoError(t, err)

	ident2, err := h.LookupDID(context.Background(), ident.DID)
	require.NoError(t, err)
	require.Equal(t, ident.DID, ident2.DID)
	require.Equal(t, ident.Handle, ident2.Handle)
}

func TestLookupDID_NotFound(t *testing.T) {
	h := newTestHive(t, "example.com", "")

	_, err := h.LookupDID(context.Background(), syntax.DID("did:web:xxxxxx.example.com"))
	require.ErrorIs(t, err, identity.ErrDIDNotFound)
}
