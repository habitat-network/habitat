package permissions

import (
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
	hasPermission, err := store.HasPermission("alice", "alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission, "owner should always have permission")

	// Test: Non-owner without permission should be denied
	hasPermission, err = store.HasPermission("bob", "alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.False(t, hasPermission, "non-owner without permission should be denied")

	// Test: Grant lexicon-level permission
	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat.posts", "")
	require.NoError(t, err)

	// Test: Bob should now have permission to all posts
	hasPermission, err = store.HasPermission("bob", "alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission, "bob should have permission after grant")

	hasPermission, err = store.HasPermission("bob", "alice", "network.habitat.posts", "record2")
	require.NoError(t, err)
	require.True(t, hasPermission, "bob should have permission to all records in the lexicon")

	// Test: Bob should not have permission to other lexicons
	hasPermission, err = store.HasPermission("bob", "alice", "network.habitat.likes", "record1")
	require.NoError(t, err)
	require.False(t, hasPermission, "bob should not have permission to other lexicons")

	// Test: Remove permission
	err = store.RemoveReadPermission("bob", "alice", "network.habitat.posts", "")
	require.NoError(t, err)

	hasPermission, err = store.HasPermission("bob", "alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.False(t, hasPermission, "bob should not have permission after removal")
}

func TestStoreMultipleGrantees(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	// Grant permissions to multiple users
	err = store.AddReadPermission([]string{"bob", "charlie"}, "alice", "network.habitat.posts", "")
	require.NoError(t, err)

	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat.likes", "")
	require.NoError(t, err)

	// List permissions by lexicon
	permissions, err := store.ListReadPermissionsByLexicon("alice")
	require.NoError(t, err)
	require.Len(t, permissions, 2)
	require.Contains(t, permissions, "network.habitat.posts")
	require.Contains(t, permissions, "network.habitat.likes")
	require.ElementsMatch(t, []string{"bob", "charlie"}, permissions["network.habitat.posts"])
	require.ElementsMatch(t, []string{"bob"}, permissions["network.habitat.likes"])
}

func TestStoreListByGrantee(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	// Grant bob access to network.habitat.posts
	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat.posts", "")
	require.NoError(t, err)

	// List bob's permissions for network.habitat.posts
	permissions, err := store.ListReadPermissionsByGrantee("bob", "network.habitat.posts")
	require.NoError(t, err)
	require.Len(t, permissions, 2)
	require.Equal(t, permissions[0].Owner, "bob")
	require.Equal(t, permissions[0].Grantee, "bob")
	require.Equal(t, permissions[0].Effect, "allow")
	require.Equal(t, permissions[1].Owner, "alice")
	require.Equal(t, permissions[1].Grantee, "bob")
	require.Equal(t, permissions[1].Collection, "network.habitat.posts")
	require.Equal(t, permissions[1].Effect, "allow")

	// Charlie has no permissions
	permissions, err = store.ListReadPermissionsByGrantee("charlie", "network.habitat.posts")
	require.NoError(t, err)
	require.Len(t, permissions, 1)
	require.Equal(t, permissions[0].Owner, "charlie")
	require.Equal(t, permissions[0].Grantee, "charlie")
	require.Equal(t, permissions[0].Effect, "allow")
}

func TestStoreEmptyRecordKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	// Grant permission
	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat.posts", "")
	require.NoError(t, err)

	// Check permission with empty record key (should check NSID-level permission)
	hasPermission, err := store.HasPermission("bob", "alice", "network.habitat.posts", "")
	require.NoError(t, err)
	require.True(t, hasPermission, "should have permission to NSID when record key is empty")
}

func TestStoreMultipleOwners(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	// Grant bob access to alice's posts
	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat.posts", "")
	require.NoError(t, err)

	// Grant bob access to charlie's likes
	err = store.AddReadPermission([]string{"bob"}, "charlie", "network.habitat.likes", "")
	require.NoError(t, err)

	// Bob should have access to alice's posts
	hasPermission, err := store.HasPermission("bob", "alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	// Bob should not have access to alice's likes
	hasPermission, err = store.HasPermission("bob", "alice", "network.habitat.likes", "record1")
	require.NoError(t, err)
	require.False(t, hasPermission)

	// Bob should have access to charlie's likes
	hasPermission, err = store.HasPermission("bob", "charlie", "network.habitat.likes", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	// List alice's permissions
	permissions, err := store.ListReadPermissionsByLexicon("alice")
	require.NoError(t, err)
	require.Len(t, permissions, 1)
	require.Contains(t, permissions, "network.habitat.posts")

	// List charlie's permissions
	permissions, err = store.ListReadPermissionsByLexicon("charlie")
	require.NoError(t, err)
	require.Len(t, permissions, 1)
	require.Contains(t, permissions, "network.habitat.likes")
}

func TestStoreDenyOverridesAllow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	// Grant bob broad access to network.habitat
	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat.posts", "")
	require.NoError(t, err)

	err = store.RemoveReadPermission("bob", "alice", "network.habitat.posts", "record1")
	require.NoError(t, err)

	// Bob should not have access to the denied post
	hasPermission, err := store.HasPermission("bob", "alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.False(t, hasPermission)

	// Bob should have access to other posts
	hasPermission, err = store.HasPermission("bob", "alice", "network.habitat.posts", "record2")
	require.NoError(t, err)
	require.True(t, hasPermission)
}

func TestPermissionStoreEmptyGrantees(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	err = store.AddReadPermission([]string{}, "alice", "network.habitat.posts", "")
	require.Error(t, err)
}

func TestAddReadPermission_EmptyCollection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	err = store.AddReadPermission([]string{"bob"}, "alice", "", "")
	require.Error(t, err)
}

func TestListReadPermissionsByGrantee_NoRedundant(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat.posts", "record-1")
	require.NoError(t, err)

	// Should not return multiple permissions for the same record 
	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat.posts", "record-1")
	require.NoError(t, err)

	perms, err := store.ListReadPermissionsByGrantee("bob", "")
	require.NoError(t, err)
	require.Len(t, perms, 2 /* includes self permissions */)

	// Should only return the most powerful permission
	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat.posts", "")
	require.NoError(t, err)

	perms, err = store.ListReadPermissionsByGrantee("bob", "")
	require.NoError(t, err)
	require.Len(t, perms, 2)
}

func 
