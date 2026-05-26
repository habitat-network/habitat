package fgastore

import (
	"context"
	"path/filepath"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/tuple"
	"github.com/stretchr/testify/require"
)

func newTestSQLite(t *testing.T) *FGA {
	t.Helper()
	f, err := NewSQLite(t.Context(), filepath.Join(t.TempDir(), "fga.db"))
	require.NoError(t, err, "NewSQLite should succeed")
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestCheck_ReturnsTrueForExistingTuple(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	err := f.Write(ctx, "user:alice", "member", "organization:myorg")
	require.NoError(t, err, "Write should succeed")

	ok, err := f.Check(ctx, "user:alice", "member", "organization:myorg")
	require.NoError(t, err, "Check should not error")
	require.True(t, ok, "Check should return true for written tuple")
}

func TestCheck_ReturnsFalseForNonExistentTuple(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	ok, err := f.Check(ctx, "user:alice", "member", "organization:myorg")
	require.NoError(t, err, "Check should not error")
	require.False(t, ok, "Check should return false for non-existent tuple")
}

func TestCheck_ReturnsFalseAfterDelete(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	err := f.Write(ctx, "user:alice", "member", "organization:myorg")
	require.NoError(t, err, "Write should succeed")

	err = f.Delete(ctx, "user:alice", "member", "organization:myorg")
	require.NoError(t, err, "Delete should succeed")

	ok, err := f.Check(ctx, "user:alice", "member", "organization:myorg")
	require.NoError(t, err, "Check should not error")
	require.False(t, ok, "Check should return false after tuple is deleted")
}

func TestCheck_DifferentRelation(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	err := f.Write(ctx, "user:alice", "member", "organization:myorg")
	require.NoError(t, err, "Write should succeed")

	ok, err := f.Check(ctx, "user:alice", "admin", "organization:myorg")
	require.NoError(t, err, "Check should not error")
	require.False(t, ok, "Check should return false for unassigned relation")
}

func TestCheck_DifferentUser(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	err := f.Write(ctx, "user:alice", "member", "organization:myorg")
	require.NoError(t, err, "Write should succeed")

	ok, err := f.Check(ctx, "user:bob", "member", "organization:myorg")
	require.NoError(t, err, "Check should not error")
	require.False(t, ok, "Check should return false for different user")
}

func TestCheck_AdminInheritsMember(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	err := f.Write(ctx, "user:alice", "admin", "organization:myorg")
	require.NoError(t, err, "Write admin tuple should succeed")

	ok, err := f.Check(ctx, "user:alice", "member", "organization:myorg")
	require.NoError(t, err, "Check should not error")
	require.True(t, ok, "org admin should be checked as member via ComputedUserset")
}

func TestCheck_AdminInheritsSpaceOwner(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	err := f.Write(ctx, "user:alice", "admin", "organization:myorg")
	require.NoError(t, err, "Write admin tuple should succeed")

	err = f.Write(ctx, "organization:myorg#admin", "owner", "space:myorg/myspace")
	require.NoError(t, err, "Write userset tuple should succeed")

	ok, err := f.Check(ctx, "user:alice", "owner", "space:myorg/myspace")
	require.NoError(t, err, "Check should not error")
	require.True(t, ok, "org admin should be checked as space owner via userset tuple resolution")
}

func TestCheck_SpaceOwnerGrantsCanWrite(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	err := f.Write(ctx, "user:alice", "owner", "space:myorg/myspace")
	require.NoError(t, err, "Write owner tuple should succeed")

	ok, err := f.Check(ctx, "user:alice", "can_write", "space:myorg/myspace")
	require.NoError(t, err, "Check should not error")
	require.True(t, ok, "space owner should have can_write via ComputedUserset")
}

func TestListObjects_ReturnsReadableSpaces(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	require.NoError(
		t,
		f.Write(ctx, "user:alice", "can_read", "space:org/a"),
		"Write space:org/a should succeed",
	)
	require.NoError(
		t,
		f.Write(ctx, "user:alice", "can_read", "space:org/b"),
		"Write space:org/b should succeed",
	)
	require.NoError(
		t,
		f.Write(ctx, "user:bob", "can_read", "space:org/c"),
		"Write space:org/c should succeed",
	)

	objects, err := f.ListObjects(ctx, "user:alice", "can_read", "space")
	require.NoError(t, err, "ListObjects should not error")
	require.ElementsMatch(
		t,
		[]string{"space:org/a", "space:org/b"},
		objects,
		"alice should see spaces a and b, not c",
	)
}

func TestListObjects_CanWriteImpliesCanRead(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	err := f.Write(ctx, "user:bob", "can_write", "space:org/x")
	require.NoError(t, err, "Write can_write should succeed")

	objects, err := f.ListObjects(ctx, "user:bob", "can_read", "space")
	require.NoError(t, err, "ListObjects should not error")
	require.Contains(t, objects, "space:org/x", "can_write user should appear in can_read list")
}

func TestListObjects_ReturnsEmptyForNoAccess(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	objects, err := f.ListObjects(ctx, "user:alice", "can_read", "space")
	require.NoError(t, err, "ListObjects should not error")
	require.Empty(t, objects, "user with no access should see empty list")
}

func TestListUsers_ReturnsReadersOfSpace(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	require.NoError(
		t,
		f.Write(ctx, "user:alice", "can_read", "space:org/myspace"),
		"Write alice as reader should succeed",
	)
	require.NoError(
		t,
		f.Write(ctx, "user:bob", "can_read", "space:org/myspace"),
		"Write bob as reader should succeed",
	)

	users, err := f.ListUsers(ctx, "space:org/myspace", "can_read")
	require.NoError(t, err, "ListUsers should not error")
	require.ElementsMatch(
		t,
		[]string{"user:alice", "user:bob"},
		users,
		"both alice and bob should be listed as readers",
	)
}

func TestListUsers_ReturnsWritersOfSpace(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	require.NoError(
		t,
		f.Write(ctx, "user:alice", "can_write", "space:org/myspace"),
		"Write alice as writer should succeed",
	)
	require.NoError(
		t,
		f.Write(ctx, "user:bob", "can_write", "space:org/myspace"),
		"Write bob as writer should succeed",
	)

	users, err := f.ListUsers(ctx, "space:org/myspace", "can_write")
	require.NoError(t, err, "ListUsers should not error")
	require.ElementsMatch(
		t,
		[]string{"user:alice", "user:bob"},
		users,
		"both alice and bob should be listed as writers",
	)
}

func TestListUsers_WriterImpliedAsReader(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	require.NoError(
		t,
		f.Write(ctx, "user:alice", "can_write", "space:org/myspace"),
		"Write alice as writer should succeed",
	)

	readers, err := f.ListUsers(ctx, "space:org/myspace", "can_read")
	require.NoError(t, err, "ListUsers should not error")
	require.Contains(t, readers, "user:alice", "writer should appear in reader list")
}

func TestListUsers_ReturnsEmptyForNoReaders(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	users, err := f.ListUsers(ctx, "space:org/myspace", "can_read")
	require.NoError(t, err, "ListUsers should not error")
	require.Empty(t, users, "space with no readers should return empty list")
}

func TestListUsers_ReturnsEmptyForNoWriters(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	users, err := f.ListUsers(ctx, "space:org/myspace", "can_write")
	require.NoError(t, err, "ListUsers should not error")
	require.Empty(t, users, "space with no writers should return empty list")
}

func TestCheck_CanReadReturnsTrueForCanWriteUser(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	err := f.Write(ctx, "user:alice", "can_write", "space:org/myspace")
	require.NoError(t, err, "Write can_write should succeed")

	ok, err := f.Check(ctx, "user:alice", "can_read", "space:org/myspace")
	require.NoError(t, err, "Check should not error")
	require.True(t, ok, "can_write user should have can_read via ComputedUserset")
}

func TestCheck_CanManageMembersGrantsCanWrite(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	err := f.Write(ctx, "user:bob", "can_manage_members", "space:org/myspace")
	require.NoError(t, err, "Write can_manage_members should succeed")

	ok, err := f.Check(ctx, "user:bob", "can_write", "space:org/myspace")
	require.NoError(t, err, "Check should not error")
	require.True(t, ok, "can_manage_members user should have can_write via ComputedUserset")

	readOK, err := f.Check(ctx, "user:bob", "can_read", "space:org/myspace")
	require.NoError(t, err, "Check should not error")
	require.True(t, readOK, "can_manage_members user should have can_read transitively")
}

func TestWriteRaw_OnDuplicateIgnore(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	tuples := []*openfgav1.TupleKey{
		tuple.NewTupleKey("space:org/x", "can_read", "user:alice"),
	}

	err := f.WriteRaw(ctx, &openfgav1.WriteRequest{
		Writes: &openfgav1.WriteRequestWrites{
			TupleKeys:   tuples,
			OnDuplicate: "ignore",
		},
	})
	require.NoError(t, err, "first WriteRaw should succeed")

	// Repeat should not error due to OnDuplicate: "ignore"
	err = f.WriteRaw(ctx, &openfgav1.WriteRequest{
		Writes: &openfgav1.WriteRequestWrites{
			TupleKeys:   tuples,
			OnDuplicate: "ignore",
		},
	})
	require.NoError(t, err, "duplicate WriteRaw with OnDuplicate: ignore should not error")

	ok, err := f.Check(ctx, "user:alice", "can_read", "space:org/x")
	require.NoError(t, err)
	require.True(t, ok, "tuple should exist after WriteRaw")
}

func TestWriteRaw_OnMissingIgnore(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	err := f.WriteRaw(ctx, &openfgav1.WriteRequest{
		Deletes: &openfgav1.WriteRequestDeletes{
			TupleKeys: []*openfgav1.TupleKeyWithoutCondition{
				tuple.TupleKeyToTupleKeyWithoutCondition(
					tuple.NewTupleKey("space:org/x", "can_read", "user:alice"),
				),
			},
			OnMissing: "ignore",
		},
	})
	require.NoError(t, err, "deleting non-existent tuple with OnMissing: ignore should not error")
}

func TestWriteRaw_ReadUpgradeToWrite(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	// Add as reader
	readKey := tuple.NewTupleKey("space:org/x", "can_read", "user:alice")
	writeKey := tuple.NewTupleKey("space:org/x", "can_write", "user:alice")
	readKeyWC := tuple.TupleKeyToTupleKeyWithoutCondition(readKey)
	writeKeyWC := tuple.TupleKeyToTupleKeyWithoutCondition(writeKey)

	err := f.WriteRaw(ctx, &openfgav1.WriteRequest{
		Writes: &openfgav1.WriteRequestWrites{
			TupleKeys:   []*openfgav1.TupleKey{readKey},
			OnDuplicate: "ignore",
		},
		Deletes: &openfgav1.WriteRequestDeletes{
			TupleKeys: []*openfgav1.TupleKeyWithoutCondition{writeKeyWC},
			OnMissing: "ignore",
		},
	})
	require.NoError(t, err)

	aliceIsReader, err := f.Check(ctx, "user:alice", "can_read", "space:org/x")
	require.NoError(t, err)
	require.True(t, aliceIsReader)
	aliceIsWriter, err := f.Check(ctx, "user:alice", "can_write", "space:org/x")
	require.NoError(t, err)
	require.False(t, aliceIsWriter)

	// Upgrade to writer
	err = f.WriteRaw(ctx, &openfgav1.WriteRequest{
		Writes: &openfgav1.WriteRequestWrites{
			TupleKeys:   []*openfgav1.TupleKey{writeKey},
			OnDuplicate: "ignore",
		},
		Deletes: &openfgav1.WriteRequestDeletes{
			TupleKeys: []*openfgav1.TupleKeyWithoutCondition{readKeyWC},
			OnMissing: "ignore",
		},
	})
	require.NoError(t, err)

	aliceIsWriter, err = f.Check(ctx, "user:alice", "can_write", "space:org/x")
	require.NoError(t, err)
	require.True(t, aliceIsWriter, "after upgrade, should be a writer")
	// Writer should still be able to read (implied by can_write -> can_read)
	aliceIsReader, err = f.Check(ctx, "user:alice", "can_read", "space:org/x")
	require.NoError(t, err)
	require.True(t, aliceIsReader, "after upgrade, should still be able to read")
}

func TestWriteRaw_DowngradeWriteToRead(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	readKey := tuple.NewTupleKey("space:org/x", "can_read", "user:alice")
	writeKey := tuple.NewTupleKey("space:org/x", "can_write", "user:alice")
	readKeyWC := tuple.TupleKeyToTupleKeyWithoutCondition(readKey)
	writeKeyWC := tuple.TupleKeyToTupleKeyWithoutCondition(writeKey)

	// Add as writer
	err := f.WriteRaw(ctx, &openfgav1.WriteRequest{
		Writes: &openfgav1.WriteRequestWrites{
			TupleKeys:   []*openfgav1.TupleKey{writeKey},
			OnDuplicate: "ignore",
		},
		Deletes: &openfgav1.WriteRequestDeletes{
			TupleKeys: []*openfgav1.TupleKeyWithoutCondition{readKeyWC},
			OnMissing: "ignore",
		},
	})
	require.NoError(t, err)

	// Downgrade to reader
	err = f.WriteRaw(ctx, &openfgav1.WriteRequest{
		Writes: &openfgav1.WriteRequestWrites{
			TupleKeys:   []*openfgav1.TupleKey{readKey},
			OnDuplicate: "ignore",
		},
		Deletes: &openfgav1.WriteRequestDeletes{
			TupleKeys: []*openfgav1.TupleKeyWithoutCondition{writeKeyWC},
			OnMissing: "ignore",
		},
	})
	require.NoError(t, err)

	aliceIsReader, err := f.Check(ctx, "user:alice", "can_read", "space:org/x")
	require.NoError(t, err)
	require.True(t, aliceIsReader, "after downgrade, should still be able to read")

	aliceIsWriter, err := f.Check(ctx, "user:alice", "can_write", "space:org/x")
	require.NoError(t, err)
	require.False(t, aliceIsWriter, "after downgrade, should no longer be a writer")
}

func TestWriteRaw_DeleteReadAndWrite(t *testing.T) {
	ctx := context.Background()
	f := newTestSQLite(t)

	readKey := tuple.NewTupleKey("space:org/x", "can_read", "user:alice")
	writeKey := tuple.NewTupleKey("space:org/x", "can_write", "user:alice")

	err := f.WriteRaw(ctx, &openfgav1.WriteRequest{
		Writes: &openfgav1.WriteRequestWrites{
			TupleKeys:   []*openfgav1.TupleKey{writeKey},
			OnDuplicate: "ignore",
		},
	})
	require.NoError(t, err)

	// Remove all access
	err = f.WriteRaw(ctx, &openfgav1.WriteRequest{
		Deletes: &openfgav1.WriteRequestDeletes{
			TupleKeys: []*openfgav1.TupleKeyWithoutCondition{
				tuple.TupleKeyToTupleKeyWithoutCondition(readKey),
				tuple.TupleKeyToTupleKeyWithoutCondition(writeKey),
			},
			OnMissing: "ignore",
		},
	})
	require.NoError(t, err)

	aliceIsReader, err := f.Check(ctx, "user:alice", "can_read", "space:org/x")
	require.NoError(t, err)
	require.False(t, aliceIsReader, "after removal, should not be able to read")

	aliceIsWriter, err := f.Check(ctx, "user:alice", "can_write", "space:org/x")
	require.NoError(t, err)
	require.False(t, aliceIsWriter, "after removal, should not be able to write")
}
