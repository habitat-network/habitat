package fgastore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewInMemory_Smoke(t *testing.T) {
	ctx := context.Background()
	f, err := NewInMemory(ctx)
	require.NoError(t, err, "NewInMemory should succeed")
	defer func() { _ = f.Close() }()
}

func TestCheck_ReturnsTrueForExistingTuple(t *testing.T) {
	ctx := context.Background()
	f, err := NewInMemory(ctx)
	require.NoError(t, err, "NewInMemory should succeed")
	defer func() { _ = f.Close() }()

	err = f.Write(ctx, "user:alice", "member", "organization:myorg")
	require.NoError(t, err, "Write should succeed")

	ok, err := f.Check(ctx, "user:alice", "member", "organization:myorg")
	require.NoError(t, err, "Check should not error")
	require.True(t, ok, "Check should return true for written tuple")
}

func TestCheck_ReturnsFalseForNonExistentTuple(t *testing.T) {
	ctx := context.Background()
	f, err := NewInMemory(ctx)
	require.NoError(t, err, "NewInMemory should succeed")
	defer func() { _ = f.Close() }()

	ok, err := f.Check(ctx, "user:alice", "member", "organization:myorg")
	require.NoError(t, err, "Check should not error")
	require.False(t, ok, "Check should return false for non-existent tuple")
}

func TestCheck_ReturnsFalseAfterDelete(t *testing.T) {
	ctx := context.Background()
	f, err := NewInMemory(ctx)
	require.NoError(t, err, "NewInMemory should succeed")
	defer func() { _ = f.Close() }()

	err = f.Write(ctx, "user:alice", "member", "organization:myorg")
	require.NoError(t, err, "Write should succeed")

	err = f.Delete(ctx, "user:alice", "member", "organization:myorg")
	require.NoError(t, err, "Delete should succeed")

	ok, err := f.Check(ctx, "user:alice", "member", "organization:myorg")
	require.NoError(t, err, "Check should not error")
	require.False(t, ok, "Check should return false after tuple is deleted")
}

func TestCheck_DifferentRelation(t *testing.T) {
	ctx := context.Background()
	f, err := NewInMemory(ctx)
	require.NoError(t, err, "NewInMemory should succeed")
	defer func() { _ = f.Close() }()

	err = f.Write(ctx, "user:alice", "member", "organization:myorg")
	require.NoError(t, err, "Write should succeed")

	ok, err := f.Check(ctx, "user:alice", "admin", "organization:myorg")
	require.NoError(t, err, "Check should not error")
	require.False(t, ok, "Check should return false for unassigned relation")
}

func TestCheck_DifferentUser(t *testing.T) {
	ctx := context.Background()
	f, err := NewInMemory(ctx)
	require.NoError(t, err, "NewInMemory should succeed")
	defer func() { _ = f.Close() }()

	err = f.Write(ctx, "user:alice", "member", "organization:myorg")
	require.NoError(t, err, "Write should succeed")

	ok, err := f.Check(ctx, "user:bob", "member", "organization:myorg")
	require.NoError(t, err, "Check should not error")
	require.False(t, ok, "Check should return false for different user")
}

func TestCheck_AdminInheritsMember(t *testing.T) {
	ctx := context.Background()
	f, err := NewInMemory(ctx)
	require.NoError(t, err, "NewInMemory should succeed")
	defer func() { _ = f.Close() }()

	err = f.Write(ctx, "user:alice", "admin", "organization:myorg")
	require.NoError(t, err, "Write admin tuple should succeed")

	ok, err := f.Check(ctx, "user:alice", "member", "organization:myorg")
	require.NoError(t, err, "Check should not error")
	require.True(t, ok, "org admin should be checked as member via ComputedUserset")
}

func TestCheck_AdminInheritsSpaceOwner(t *testing.T) {
	ctx := context.Background()
	f, err := NewInMemory(ctx)
	require.NoError(t, err, "NewInMemory should succeed")
	defer func() { _ = f.Close() }()

	err = f.Write(ctx, "user:alice", "admin", "organization:myorg")
	require.NoError(t, err, "Write admin tuple should succeed")

	err = f.Write(ctx, "organization:myorg#admin", "owner", "space:myorg/myspace")
	require.NoError(t, err, "Write userset tuple should succeed")

	ok, err := f.Check(ctx, "user:alice", "owner", "space:myorg/myspace")
	require.NoError(t, err, "Check should not error")
	require.True(t, ok, "org admin should be checked as space owner via userset tuple resolution")
}

func TestCheck_SpaceOwnerGrantsCanDelete(t *testing.T) {
	ctx := context.Background()
	f, err := NewInMemory(ctx)
	require.NoError(t, err, "NewInMemory should succeed")
	defer func() { _ = f.Close() }()

	err = f.Write(ctx, "user:alice", "owner", "space:myorg/myspace")
	require.NoError(t, err, "Write owner tuple should succeed")

	ok, err := f.Check(ctx, "user:alice", "can_delete", "space:myorg/myspace")
	require.NoError(t, err, "Check should not error")
	require.True(t, ok, "space owner should have can_delete via ComputedUserset")
}

func TestListObjects_ReturnsMemberSpaces(t *testing.T) {
	ctx := context.Background()
	f, err := NewInMemory(ctx)
	require.NoError(t, err, "NewInMemory should succeed")
	defer func() { _ = f.Close() }()

	require.NoError(
		t,
		f.Write(ctx, "user:alice", "member", "space:org/a"),
		"Write space:org/a should succeed",
	)
	require.NoError(
		t,
		f.Write(ctx, "user:alice", "member", "space:org/b"),
		"Write space:org/b should succeed",
	)
	require.NoError(
		t,
		f.Write(ctx, "user:bob", "member", "space:org/c"),
		"Write space:org/c should succeed",
	)

	objects, err := f.ListObjects(ctx, "user:alice", "member", "space")
	require.NoError(t, err, "ListObjects should not error")
	require.ElementsMatch(
		t,
		[]string{"space:org/a", "space:org/b"},
		objects,
		"alice should see spaces a and b, not c",
	)
}

func TestListObjects_ReturnsEmptyForNoMembership(t *testing.T) {
	ctx := context.Background()
	f, err := NewInMemory(ctx)
	require.NoError(t, err, "NewInMemory should succeed")
	defer func() { _ = f.Close() }()

	objects, err := f.ListObjects(ctx, "user:alice", "member", "space")
	require.NoError(t, err, "ListObjects should not error")
	require.Empty(t, objects, "user with no memberships should see empty list")
}

func TestListUsers_ReturnsMembersOfSpace(t *testing.T) {
	ctx := context.Background()
	f, err := NewInMemory(ctx)
	require.NoError(t, err, "NewInMemory should succeed")
	defer func() { _ = f.Close() }()

	require.NoError(
		t,
		f.Write(ctx, "user:alice", "member", "space:org/myspace"),
		"Write alice as member should succeed",
	)
	require.NoError(
		t,
		f.Write(ctx, "user:bob", "member", "space:org/myspace"),
		"Write bob as member should succeed",
	)

	users, err := f.ListUsers(ctx, "space:org/myspace", "member")
	require.NoError(t, err, "ListUsers should not error")
	require.ElementsMatch(
		t,
		[]string{"user:alice", "user:bob"},
		users,
		"both alice and bob should be listed as members",
	)
}

func TestListUsers_ReturnsEmptyForNoMembers(t *testing.T) {
	ctx := context.Background()
	f, err := NewInMemory(ctx)
	require.NoError(t, err, "NewInMemory should succeed")
	defer func() { _ = f.Close() }()

	users, err := f.ListUsers(ctx, "space:org/myspace", "member")
	require.NoError(t, err, "ListUsers should not error")
	require.Empty(t, users, "space with no members should return empty list")
}
