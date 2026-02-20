package permissions

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestStoreBasicPermissions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	// Test: Owner always has permission
	HasDirectPermission, err := store.HasDirectPermission("did:example:alice", "did:example:alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, HasDirectPermission, "owner should always have permission")

	// Test: Non-owner without permission should be denied
	HasDirectPermission, err = store.HasDirectPermission("did:example:bob", "did:example:alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.False(t, HasDirectPermission, "non-owner without permission should be denied")

	// Test: Grant lexicon-level permission
	err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)

	// Test: Bob should now have permission to all posts
	HasDirectPermission, err = store.HasDirectPermission("did:example:bob", "did:example:alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, HasDirectPermission, "bob should have permission after grant")

	HasDirectPermission, err = store.HasDirectPermission("did:example:bob", "did:example:alice", "network.habitat.posts", "record2")
	require.NoError(t, err)
	require.True(t, HasDirectPermission, "bob should have permission to all records in the lexicon")

	// Test: Bob should not have permission to other lexicons
	HasDirectPermission, err = store.HasDirectPermission("did:example:bob", "did:example:alice", "network.habitat.likes", "record1")
	require.NoError(t, err)
	require.False(t, HasDirectPermission, "bob should not have permission to other lexicons")

	// Test: Remove permission
	err = store.RemovePermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)

	HasDirectPermission, err = store.HasDirectPermission("did:example:bob", "did:example:alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.False(t, HasDirectPermission, "bob should not have permission after removal")
}

func TestStoreMultipleGrantees(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	// Grant permissions to multiple users
	err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob"), DIDGrantee("did:example:charlie")}, "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)

	err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.likes", "")
	require.NoError(t, err)

	// List permissions by lexicon
	permissions, err := store.ListPermissions("", "did:example:alice", "", "")
	require.NoError(t, err)
	// Alice gave three permission grants
	require.Len(t, permissions, 3)
}

func TestStoreListByGrantee(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	// Grant bob access to network.habitat.posts
	err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)

	// List bob's permissions for network.habitat.posts
	permissions, err := store.ListPermissions("did:example:bob", "", "network.habitat.posts", "")
	slices.SortFunc(permissions, func(a, b Permission) int {
		if a.Owner < b.Owner {
			return -1
		} else if a.Owner == b.Owner {
			return 0
		}
		return 1
	})
	require.NoError(t, err)
	require.Len(t, permissions, 2)
	require.Equal(t, permissions[0].Owner.String(), "did:example:alice")
	require.Equal(t, permissions[0].Grantee.String(), "did:example:bob")
	require.Equal(t, permissions[0].Effect, Allow)
	require.Equal(t, permissions[1].Owner.String(), "did:example:bob")
	require.Equal(t, permissions[1].Grantee.String(), "did:example:bob")
	require.Equal(t, permissions[1].Collection.String(), "network.habitat.posts")
	require.Equal(t, permissions[1].Effect, Allow)

	// Charlie has no permissions
	permissions, err = store.ListPermissions("did:example:charlie", "", "network.habitat.posts", "")
	require.NoError(t, err)
	require.Len(t, permissions, 1)
	require.Equal(t, permissions[0].Owner.String(), "did:example:charlie")
	require.Equal(t, permissions[0].Grantee.String(), "did:example:charlie")
	require.Equal(t, permissions[0].Effect, Allow)
}

func TestStoreEmptyRecordKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	// Grant permission
	err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)

	// Check permission with empty record key (should check NSID-level permission)
	HasDirectPermission, err := store.HasDirectPermission("did:example:bob", "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)
	require.True(t, HasDirectPermission, "should have permission to NSID when record key is empty")
}

func TestStoreMultipleOwners(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	// Grant bob access to alice's posts
	err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)

	// Grant bob access to charlie's likes
	err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:charlie", "network.habitat.likes", "")
	require.NoError(t, err)

	// Bob should have access to alice's posts
	HasDirectPermission, err := store.HasDirectPermission("did:example:bob", "did:example:alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, HasDirectPermission)

	// Bob should not have access to alice's likes
	HasDirectPermission, err = store.HasDirectPermission("did:example:bob", "did:example:alice", "network.habitat.likes", "record1")
	require.NoError(t, err)
	require.False(t, HasDirectPermission)

	// Bob should have access to charlie's likes
	HasDirectPermission, err = store.HasDirectPermission("did:example:bob", "did:example:charlie", "network.habitat.likes", "record1")
	require.NoError(t, err)
	require.True(t, HasDirectPermission)

	// List alice's permissions
	permissions, err := store.ListPermissions("", "did:example:alice", "", "")
	require.NoError(t, err)
	require.Len(t, permissions, 1)
	require.Equal(t, Permission{Grantee: DIDGrantee("did:example:bob"), Owner: "did:example:alice", Collection: "network.habitat.posts", Rkey: "", Effect: Allow}, permissions[0])

	// List charlie's permissions
	permissions, err = store.ListPermissions("", "did:example:charlie", "", "")
	require.NoError(t, err)
	require.Len(t, permissions, 1)
	require.Equal(t, Permission{Grantee: DIDGrantee("did:example:bob"), Owner: "did:example:charlie", Collection: "network.habitat.likes", Rkey: "", Effect: Allow}, permissions[0])
}

func TestStoreDenyOverridesAllow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	// Grant bob broad access to network.habitat
	err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)

	err = store.RemovePermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "record1")
	require.NoError(t, err)

	// Bob should not have access to the denied post
	HasDirectPermission, err := store.HasDirectPermission("did:example:bob", "did:example:alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.False(t, HasDirectPermission)

	// Bob should have access to other posts
	HasDirectPermission, err = store.HasDirectPermission("did:example:bob", "did:example:alice", "network.habitat.posts", "record2")
	require.NoError(t, err)
	require.True(t, HasDirectPermission)
}

func TestAddReadPermission_EmptyCollection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "", "")
	require.Error(t, err)
}

func TestListReadPermissionsByGrantee_NoRedundant(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "record-1")
	require.NoError(t, err)

	// Should not return multiple permissions for the same record
	err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "record-1")
	require.NoError(t, err)

	perms, err := store.ListPermissions("did:example:bob", "", "", "")
	require.NoError(t, err)
	require.Len(t, perms, 1 /* has direct permission on record */)

	// Should only return the most powerful permission
	err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)

	perms, err = store.ListPermissions("did:example:bob", "", "", "")
	require.NoError(t, err)
	require.Len(t, perms, 1 /* no collection specified so only returns direct permissions */)

	// should not add unnecessary permissions
	err = store.AddPermissions(
		[]Grantee{DIDGrantee("did:example:bob"), DIDGrantee("did:example:charlie")},
		"did:example:alice",
		"network.habitat.posts",
		"record-1",
	)
	require.NoError(t, err)

	perms, err = store.ListPermissions("did:example:bob", "", "", "")
	require.NoError(t, err)
	require.Len(t, perms, 1 /* has direct permission on record */)
}
