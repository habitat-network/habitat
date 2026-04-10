package org

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/pkg/org"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestStore(t *testing.T) *store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	s, err := NewStore(org.Org{Domain: "test.example.com"}, db)
	require.NoError(t, err)
	return s.(*store)
}

var (
	did1 = syntax.DID("did:plc:alice111")
	did2 = syntax.DID("did:plc:bob2222")
)

func TestIsMember(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	ok, err := s.IsMember(ctx, did1)
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, s.addMembers(ctx, did1, []syntax.DID{did1}))

	ok, err = s.IsMember(ctx, did1)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestAddAdmin_GetAdmins(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.addAdmin(ctx, did1, did1))

	admins, err := s.getAdmins(ctx)
	require.NoError(t, err)
	require.Equal(t, []syntax.DID{did1}, admins)
}

func TestRemoveAdmin_LastAdmin(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.addAdmin(ctx, did1, did1))

	err := s.removeAdmin(ctx, did1, did1)
	require.ErrorIs(t, err, ErrLastAdmin)
}

func TestRemoveAdmin_MultipleAdmins(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.addAdmin(ctx, did1, did1))
	require.NoError(t, s.addAdmin(ctx, did1, did2))

	require.NoError(t, s.removeAdmin(ctx, did1, did2))

	admins, err := s.getAdmins(ctx)
	require.NoError(t, err)
	require.Equal(t, []syntax.DID{did1}, admins)
}

func TestGetMembers(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	members, err := s.getMembers(ctx)
	require.NoError(t, err)
	require.Empty(t, members)

	require.NoError(t, s.addMembers(ctx, did1, []syntax.DID{did1, did2}))

	members, err = s.getMembers(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []syntax.DID{did1, did2}, members)
}

func TestRemoveMembers(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.addMembers(ctx, did1, []syntax.DID{did1, did2}))

	require.NoError(t, s.removeMembers(ctx, did1, []syntax.DID{did2}))

	ok, err := s.IsMember(ctx, did1)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = s.IsMember(ctx, did2)
	require.NoError(t, err)
	require.False(t, ok)
}
