package permissions_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/clique"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) permissions.Store {
	db := testutil.NewPearDB(t)
	cliqueStore, err := clique.NewStore(db)
	require.NoError(t, err)
	store, err := permissions.NewStore(db, cliqueStore)
	require.NoError(t, err)
	return store
}

func TestStoreBasicPermissions(t *testing.T) {
	store := newTestStore(t)

	// Test: Owner always has permission
	HasPermission, err := store.HasPermission(
		t.Context(),
		"did:example:alice",
		"did:example:alice",
		"network.habitat.posts",
		"record1",
	)
	require.NoError(t, err)
	require.True(t, HasPermission, "owner should always have permission")

	// Test: Non-owner without permission should be denied
	HasPermission, err = store.HasPermission(
		t.Context(),
		"did:example:bob",
		"did:example:alice",
		"network.habitat.posts",
		"record1",
	)
	require.NoError(t, err)
	require.False(t, HasPermission, "non-owner without permission should be denied")

	// Test: Grant record-level permission
	err = store.AddPermissions(
		t.Context(),
		[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
		"did:example:alice",
		"network.habitat.posts",
		"record1",
	)
	require.NoError(t, err)

	// Test: Bob should now have permission to record1
	HasPermission, err = store.HasPermission(
		t.Context(),
		"did:example:bob",
		"did:example:alice",
		"network.habitat.posts",
		"record1",
	)
	require.NoError(t, err)
	require.True(t, HasPermission, "bob should have permission after grant")

	// Test: Bob should not have permission to other records
	HasPermission, err = store.HasPermission(
		t.Context(),
		"did:example:bob",
		"did:example:alice",
		"network.habitat.posts",
		"record2",
	)
	require.NoError(t, err)
	require.False(t, HasPermission, "bob should not have permission to other records")

	// Test: Bob should not have permission to other collections
	HasPermission, err = store.HasPermission(
		t.Context(),
		"did:example:bob",
		"did:example:alice",
		"network.habitat.likes",
		"record1",
	)
	require.NoError(t, err)
	require.False(t, HasPermission, "bob should not have permission to other collections")

	// Test: Remove record-level permission
	err = store.RemovePermissions(
		t.Context(),
		[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
		"did:example:alice",
		"network.habitat.posts",
		"record1",
	)
	require.NoError(t, err)

	HasPermission, err = store.HasPermission(
		t.Context(),
		"did:example:bob",
		"did:example:alice",
		"network.habitat.posts",
		"record1",
	)
	require.NoError(t, err)
	require.False(t, HasPermission, "bob should not have permission after removal")
}

func TestStoreMultipleGrantees(t *testing.T) {
	store := newTestStore(t)

	// Grant permissions to multiple users for different records
	err := store.AddPermissions(
		t.Context(),
		[]permissions.Grantee{
			permissions.DIDGrantee("did:example:bob"),
			permissions.DIDGrantee("did:example:charlie"),
		},
		"did:example:alice",
		"network.habitat.posts",
		"record1",
	)
	require.NoError(t, err)

	err = store.AddPermissions(
		t.Context(),
		[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
		"did:example:alice",
		"network.habitat.likes",
		"record1",
	)
	require.NoError(t, err)

	// List permissions by owner
	got, err := permissions.ListPermissions(store,
		t.Context(),
		[]permissions.Grantee{},
		[]syntax.DID{"did:example:alice"},
		"",
		"",
	)
	require.NoError(t, err)
	// Alice gave three permission grants
	require.Len(t, got, 3)
}

func TestStoreListByGrantee(t *testing.T) {
	store := newTestStore(t)

	// Grant bob access to a specific record in network.habitat.posts
	err := store.AddPermissions(
		t.Context(),
		[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
		"did:example:alice",
		"network.habitat.posts",
		"record1",
	)
	require.NoError(t, err)

	// List bob's permissions for network.habitat.posts
	got, err := permissions.ListPermissions(store,
		t.Context(),
		[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
		nil,
		"network.habitat.posts",
		"",
	)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "did:example:alice", got[0].Owner.String())
	require.Equal(t, "did:example:bob", got[0].Grantee.String())

	// Charlie has no permissions
	got, err = permissions.ListPermissions(store,
		t.Context(),
		[]permissions.Grantee{permissions.DIDGrantee("did:example:charlie")},
		nil,
		"network.habitat.posts",
		"",
	)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestStoreCollectionLevelNotSupported(t *testing.T) {
	store := newTestStore(t)

	// AddPermissions with empty rkey should return permissions.ErrCollectionLevelNotSupported
	err := store.AddPermissions(
		t.Context(),
		[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
		"did:example:alice",
		"network.habitat.posts",
		"",
	)
	require.ErrorIs(t, err, permissions.ErrCollectionLevelNotSupported)

	// RemovePermissions with empty rkey should return permissions.ErrCollectionLevelNotSupported
	err = store.RemovePermissions(
		t.Context(),
		[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
		"did:example:alice",
		"network.habitat.posts",
		"",
	)
	require.ErrorIs(t, err, permissions.ErrCollectionLevelNotSupported)

	// HasPermission with empty rkey should return permissions.ErrCollectionLevelNotSupported
	_, err = store.HasPermission(
		t.Context(),
		"did:example:bob",
		"did:example:alice",
		"network.habitat.posts",
		"",
	)
	require.ErrorIs(t, err, permissions.ErrCollectionLevelNotSupported)
}

func TestStoreMultipleOwners(t *testing.T) {
	store := newTestStore(t)

	// Grant bob access to alice's post
	err := store.AddPermissions(
		t.Context(),
		[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
		"did:example:alice",
		"network.habitat.posts",
		"record1",
	)
	require.NoError(t, err)

	// Grant bob access to charlie's like
	err = store.AddPermissions(
		t.Context(),
		[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
		"did:example:charlie",
		"network.habitat.likes",
		"record1",
	)
	require.NoError(t, err)

	// Bob should have access to alice's post
	HasPermission, err := store.HasPermission(
		t.Context(),
		"did:example:bob",
		"did:example:alice",
		"network.habitat.posts",
		"record1",
	)
	require.NoError(t, err)
	require.True(t, HasPermission)

	// Bob should not have access to alice's likes
	HasPermission, err = store.HasPermission(
		t.Context(),
		"did:example:bob",
		"did:example:alice",
		"network.habitat.likes",
		"record1",
	)
	require.NoError(t, err)
	require.False(t, HasPermission)

	// Bob should have access to charlie's like
	HasPermission, err = store.HasPermission(
		t.Context(),
		"did:example:bob",
		"did:example:charlie",
		"network.habitat.likes",
		"record1",
	)
	require.NoError(t, err)
	require.True(t, HasPermission)

	// List alice's permissions
	got, err := permissions.ListPermissions(store,
		t.Context(),
		[]permissions.Grantee{},
		[]syntax.DID{"did:example:alice"},
		"",
		"",
	)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(
		t,
		permissions.Permission{
			Grantee:    permissions.DIDGrantee("did:example:bob"),
			Owner:      "did:example:alice",
			Collection: "network.habitat.posts",
			Rkey:       "record1",
		},
		got[0],
	)

	// List charlie's permissions
	got, err = permissions.ListPermissions(store,
		t.Context(),
		[]permissions.Grantee{},
		[]syntax.DID{"did:example:charlie"},
		"",
		"",
	)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(
		t,
		permissions.Permission{
			Grantee:    permissions.DIDGrantee("did:example:bob"),
			Owner:      "did:example:charlie",
			Collection: "network.habitat.likes",
			Rkey:       "record1",
		},
		got[0],
	)
}

func TestStoreRemovePermission(t *testing.T) {
	store := newTestStore(t)

	err := store.AddPermissions(
		t.Context(),
		[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
		"did:example:alice",
		"network.habitat.posts",
		"record1",
	)
	require.NoError(t, err)

	HasPermission, err := store.HasPermission(
		t.Context(),
		"did:example:bob",
		"did:example:alice",
		"network.habitat.posts",
		"record1",
	)
	require.NoError(t, err)
	require.True(t, HasPermission)

	err = store.RemovePermissions(
		t.Context(),
		[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
		"did:example:alice",
		"network.habitat.posts",
		"record1",
	)
	require.NoError(t, err)

	HasPermission, err = store.HasPermission(
		t.Context(),
		"did:example:bob",
		"did:example:alice",
		"network.habitat.posts",
		"record1",
	)
	require.NoError(t, err)
	require.False(t, HasPermission)
}

func TestListAllowedGranteesForRecord(t *testing.T) {
	t.Run("basic: grantee with permission is returned", func(t *testing.T) {
		store := newTestStore(t)
		err := store.AddPermissions(
			t.Context(),
			[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
			"did:example:alice",
			"network.habitat.posts",
			"record1",
		)
		require.NoError(t, err)

		grants, err := store.ListAllowedGranteesForRecord(
			t.Context(),
			"did:example:alice",
			"network.habitat.posts",
			"record1",
		)
		require.NoError(t, err)
		require.Len(t, grants, 1)
		require.Equal(t, permissions.DIDGrantee("did:example:bob"), grants[0])
	})

	t.Run("no permission returns empty", func(t *testing.T) {
		store := newTestStore(t)

		perms, err := store.ListAllowedGranteesForRecord(
			t.Context(),
			"did:example:alice",
			"network.habitat.posts",
			"record1",
		)
		require.NoError(t, err)
		require.Empty(t, perms)
	})

	t.Run("removed permission is not returned", func(t *testing.T) {
		store := newTestStore(t)

		err := store.AddPermissions(
			t.Context(),
			[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
			"did:example:alice",
			"network.habitat.posts",
			"record1",
		)
		require.NoError(t, err)

		err = store.RemovePermissions(
			t.Context(),
			[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
			"did:example:alice",
			"network.habitat.posts",
			"record1",
		)
		require.NoError(t, err)

		perms, err := store.ListAllowedGranteesForRecord(
			t.Context(),
			"did:example:alice",
			"network.habitat.posts",
			"record1",
		)
		require.NoError(t, err)
		require.Empty(t, perms)
	})

	t.Run("permission on different record does not appear", func(t *testing.T) {
		store := newTestStore(t)

		err := store.AddPermissions(
			t.Context(),
			[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
			"did:example:alice",
			"network.habitat.posts",
			"record2",
		)
		require.NoError(t, err)

		grants, err := store.ListAllowedGranteesForRecord(
			t.Context(),
			"did:example:alice",
			"network.habitat.posts",
			"record1",
		)
		require.NoError(t, err)
		require.Empty(t, grants)
	})
}

func TestHasPermissionNoMatchingCollectionAndRkeyHasRandomClique(t *testing.T) {
	store := newTestStore(t)

	err := store.AddPermissions(
		t.Context(),
		[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
		"did:example:alice",
		"network.habitat.clique",
		"alice-cliqeu",
	)
	require.NoError(t, err)

	// No permissions have been added for this collection+rkey
	hasPermission, err := store.HasPermission(
		t.Context(),
		"did:example:bob",
		"did:example:alice",
		"network.habitat.posts",
		"record1",
	)
	require.NoError(t, err)
	require.False(
		t,
		hasPermission,
		"should return false when no permissions match the collection+rkey",
	)
}

func TestAddReadPermission_EmptyCollection(t *testing.T) {
	store := newTestStore(t)

	err := store.AddPermissions(
		t.Context(),
		[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
		"did:example:alice",
		"",
		"",
	)
	require.Error(t, err)
}

func TestListReadPermissionsByGrantee_NoRedundant(t *testing.T) {
	store := newTestStore(t)

	err := store.AddPermissions(
		t.Context(),
		[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
		"did:example:alice",
		"network.habitat.posts",
		"record-1",
	)
	require.NoError(t, err)

	// Adding the same permission again should not create a duplicate
	err = store.AddPermissions(
		t.Context(),
		[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
		"did:example:alice",
		"network.habitat.posts",
		"record-1",
	)
	require.NoError(t, err)

	perms, err := permissions.ListPermissions(store,
		t.Context(),
		[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
		nil,
		"",
		"",
	)
	require.NoError(t, err)
	require.Len(t, perms, 1)

	// Adding another grantee for the same record should not affect bob's count
	err = store.AddPermissions(
		t.Context(),
		[]permissions.Grantee{
			permissions.DIDGrantee("did:example:bob"),
			permissions.DIDGrantee("did:example:charlie"),
		},
		"did:example:alice",
		"network.habitat.posts",
		"record-1",
	)
	require.NoError(t, err)

	perms, err = permissions.ListPermissions(store,
		t.Context(),
		[]permissions.Grantee{permissions.DIDGrantee("did:example:bob")},
		nil,
		"",
		"",
	)
	require.NoError(t, err)
	require.Len(t, perms, 1)
}
