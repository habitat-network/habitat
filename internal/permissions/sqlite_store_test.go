package permissions

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSQLiteStoreBasicPermissions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewSQLiteStore(db)
	require.NoError(t, err)

	// Test: Owner always has permission
	hasPermission, err := store.HasPermission("alice", "alice", "com.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission, "owner should always have permission")

	// Test: Non-owner without permission should be denied
	hasPermission, err = store.HasPermission("bob", "alice", "com.habitat.posts", "record1")
	require.NoError(t, err)
	require.False(t, hasPermission, "non-owner without permission should be denied")

	// Test: Grant lexicon-level permission
	err = store.AddLexiconReadPermission("bob", "alice", "com.habitat.posts")
	require.NoError(t, err)

	// Test: Bob should now have permission to all posts
	hasPermission, err = store.HasPermission("bob", "alice", "com.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission, "bob should have permission after grant")

	hasPermission, err = store.HasPermission("bob", "alice", "com.habitat.posts", "record2")
	require.NoError(t, err)
	require.True(t, hasPermission, "bob should have permission to all records in the lexicon")

	// Test: Bob should not have permission to other lexicons
	hasPermission, err = store.HasPermission("bob", "alice", "com.habitat.likes", "record1")
	require.NoError(t, err)
	require.False(t, hasPermission, "bob should not have permission to other lexicons")

	// Test: Remove permission
	err = store.RemoveLexiconReadPermission("bob", "alice", "com.habitat.posts")
	require.NoError(t, err)

	hasPermission, err = store.HasPermission("bob", "alice", "com.habitat.posts", "record1")
	require.NoError(t, err)
	require.False(t, hasPermission, "bob should not have permission after removal")
}

func TestSQLiteStorePrefixPermissions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewSQLiteStore(db)
	require.NoError(t, err)

	// Grant permission to all "com.habitat.*" lexicons
	err = store.AddLexiconReadPermission("bob", "alice", "com.habitat")
	require.NoError(t, err)

	// Bob should have access to any lexicon under com.habitat
	hasPermission, err := store.HasPermission("bob", "alice", "com.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	hasPermission, err = store.HasPermission("bob", "alice", "com.habitat.likes", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	hasPermission, err = store.HasPermission("bob", "alice", "com.habitat.follows", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	// Bob should not have access to other top-level domains
	hasPermission, err = store.HasPermission("bob", "alice", "org.example.posts", "record1")
	require.NoError(t, err)
	require.False(t, hasPermission)
}

func TestSQLiteStoreMultipleGrantees(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewSQLiteStore(db)
	require.NoError(t, err)

	// Grant permissions to multiple users
	err = store.AddLexiconReadPermission("bob", "alice", "com.habitat.posts")
	require.NoError(t, err)

	err = store.AddLexiconReadPermission("charlie", "alice", "com.habitat.posts")
	require.NoError(t, err)

	err = store.AddLexiconReadPermission("bob", "alice", "com.habitat.likes")
	require.NoError(t, err)

	// List permissions by lexicon
	permissions, err := store.ListReadPermissionsByLexicon("alice")
	require.NoError(t, err)
	require.Len(t, permissions, 2)
	require.Contains(t, permissions, "com.habitat.posts")
	require.Contains(t, permissions, "com.habitat.likes")
	require.ElementsMatch(t, []string{"bob", "charlie"}, permissions["com.habitat.posts"])
	require.ElementsMatch(t, []string{"bob"}, permissions["com.habitat.likes"])
}

func TestSQLiteStoreListByUser(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewSQLiteStore(db)
	require.NoError(t, err)

	// Grant bob access to com.habitat.posts
	err = store.AddLexiconReadPermission("bob", "alice", "com.habitat.posts")
	require.NoError(t, err)

	// List bob's permissions for com.habitat.posts
	allows, denies, err := store.ListReadPermissionsByUser("alice", "bob", "com.habitat.posts")
	require.NoError(t, err)
	require.Len(t, allows, 1)
	require.Contains(t, allows, "com.habitat.posts")
	require.Len(t, denies, 0)

	// Charlie has no permissions
	allows, denies, err = store.ListReadPermissionsByUser("alice", "charlie", "com.habitat.posts")
	require.NoError(t, err)
	require.Len(t, allows, 0)
	require.Len(t, denies, 0)
}

func TestSQLiteStorePermissionHierarchy(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewSQLiteStore(db)
	require.NoError(t, err)

	// Grant broad permission
	err = store.AddLexiconReadPermission("bob", "alice", "com.habitat")
	require.NoError(t, err)

	// Grant more specific permission
	err = store.AddLexiconReadPermission("charlie", "alice", "com.habitat.posts")
	require.NoError(t, err)

	// Bob has access via broad permission
	hasPermission, err := store.HasPermission("bob", "alice", "com.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	hasPermission, err = store.HasPermission("bob", "alice", "com.habitat.likes", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	// Charlie only has access to posts
	hasPermission, err = store.HasPermission("charlie", "alice", "com.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	hasPermission, err = store.HasPermission("charlie", "alice", "com.habitat.likes", "record1")
	require.NoError(t, err)
	require.False(t, hasPermission)
}

func TestSQLiteStoreEmptyRecordKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewSQLiteStore(db)
	require.NoError(t, err)

	// Grant permission
	err = store.AddLexiconReadPermission("bob", "alice", "com.habitat.posts")
	require.NoError(t, err)

	// Check permission with empty record key (should check NSID-level permission)
	hasPermission, err := store.HasPermission("bob", "alice", "com.habitat.posts", "")
	require.NoError(t, err)
	require.True(t, hasPermission, "should have permission to NSID when record key is empty")
}

func TestSQLiteStoreMultipleOwners(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewSQLiteStore(db)
	require.NoError(t, err)

	// Grant bob access to alice's posts
	err = store.AddLexiconReadPermission("bob", "alice", "com.habitat.posts")
	require.NoError(t, err)

	// Grant bob access to charlie's likes
	err = store.AddLexiconReadPermission("bob", "charlie", "com.habitat.likes")
	require.NoError(t, err)

	// Bob should have access to alice's posts
	hasPermission, err := store.HasPermission("bob", "alice", "com.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	// Bob should not have access to alice's likes
	hasPermission, err = store.HasPermission("bob", "alice", "com.habitat.likes", "record1")
	require.NoError(t, err)
	require.False(t, hasPermission)

	// Bob should have access to charlie's likes
	hasPermission, err = store.HasPermission("bob", "charlie", "com.habitat.likes", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	// List alice's permissions
	permissions, err := store.ListReadPermissionsByLexicon("alice")
	require.NoError(t, err)
	require.Len(t, permissions, 1)
	require.Contains(t, permissions, "com.habitat.posts")

	// List charlie's permissions
	permissions, err = store.ListReadPermissionsByLexicon("charlie")
	require.NoError(t, err)
	require.Len(t, permissions, 1)
	require.Contains(t, permissions, "com.habitat.likes")
}

func TestSQLiteStoreDenyOverridesAllow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewSQLiteStore(db)
	require.NoError(t, err)

	// Grant bob broad access to com.habitat
	err = store.AddLexiconReadPermission("bob", "alice", "com.habitat")
	require.NoError(t, err)

	// Bob should have access to posts
	hasPermission, err := store.HasPermission("bob", "alice", "com.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	// Bob should have access to likes
	hasPermission, err = store.HasPermission("bob", "alice", "com.habitat.likes", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	// Now add a deny rule for likes specifically using GORM
	denyPermission := Permission{
		Grantee: "bob",
		Owner:   "alice",
		Object:  "com.habitat.likes",
		Effect:  "deny",
	}
	err = db.Create(&denyPermission).Error
	require.NoError(t, err)

	// Bob should still have access to posts
	hasPermission, err = store.HasPermission("bob", "alice", "com.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	// Bob should now be denied access to likes (deny overrides broader allow)
	hasPermission, err = store.HasPermission("bob", "alice", "com.habitat.likes", "record1")
	require.NoError(t, err)
	require.False(t, hasPermission, "deny should override broader allow")

	// Bob should also be denied access to specific like records
	hasPermission, err = store.HasPermission("bob", "alice", "com.habitat.likes", "specific-record")
	require.NoError(t, err)
	require.False(t, hasPermission, "deny should apply to all records under likes")
}
