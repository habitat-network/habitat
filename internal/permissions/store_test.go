package permissions

import (
	"context"
	"testing"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
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
	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat.posts")
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
	err = store.RemoveReadPermission("bob", "alice", "network.habitat.posts")
	require.NoError(t, err)

	hasPermission, err = store.HasPermission("bob", "alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.False(t, hasPermission, "bob should not have permission after removal")
}

func TestStorePrefixPermissions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	// Grant permission to all "network.habitat.*" lexicons
	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat")
	require.NoError(t, err)

	// Bob should have access to any lexicon under network.habitat
	hasPermission, err := store.HasPermission("bob", "alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	hasPermission, err = store.HasPermission("bob", "alice", "network.habitat.likes", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	hasPermission, err = store.HasPermission("bob", "alice", "network.habitat.follows", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	// Bob should not have access to other top-level domains
	hasPermission, err = store.HasPermission("bob", "alice", "org.example.posts", "record1")
	require.NoError(t, err)
	require.False(t, hasPermission)
}

func TestStoreMultipleGrantees(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	// Grant permissions to multiple users
	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat.posts")
	require.NoError(t, err)

	err = store.AddReadPermission([]string{"charlie"}, "alice", "network.habitat.posts")
	require.NoError(t, err)

	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat.likes")
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

func TestStoreListByUser(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	// Grant bob access to network.habitat.posts
	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat.posts")
	require.NoError(t, err)

	// List bob's permissions for network.habitat.posts
	allows, denies, err := store.ListReadPermissionsByUser("alice", "bob", "network.habitat.posts")
	require.NoError(t, err)
	require.Len(t, allows, 1)
	require.Contains(t, allows, "network.habitat.posts")
	require.Len(t, denies, 0)

	// Charlie has no permissions
	allows, denies, err = store.ListReadPermissionsByUser(
		"alice",
		"charlie",
		"network.habitat.posts",
	)
	require.NoError(t, err)
	require.Len(t, allows, 0)
	require.Len(t, denies, 0)
}

func TestStorePermissionHierarchy(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	// Grant broad permission
	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat")
	require.NoError(t, err)

	// Grant more specific permission
	err = store.AddReadPermission([]string{"charlie"}, "alice", "network.habitat.posts")
	require.NoError(t, err)

	// Bob has access via broad permission
	hasPermission, err := store.HasPermission("bob", "alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	hasPermission, err = store.HasPermission("bob", "alice", "network.habitat.likes", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	// Charlie only has access to posts
	hasPermission, err = store.HasPermission("charlie", "alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	hasPermission, err = store.HasPermission("charlie", "alice", "network.habitat.likes", "record1")
	require.NoError(t, err)
	require.False(t, hasPermission)
}

func TestStoreEmptyRecordKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	// Grant permission
	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat.posts")
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
	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat.posts")
	require.NoError(t, err)

	// Grant bob access to charlie's likes
	err = store.AddReadPermission([]string{"bob"}, "charlie", "network.habitat.likes")
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
	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat")
	require.NoError(t, err)

	// Bob should have access to posts
	hasPermission, err := store.HasPermission("bob", "alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	// Bob should have access to likes
	hasPermission, err = store.HasPermission("bob", "alice", "network.habitat.likes", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	// Now add a deny rule for likes specifically using GORM
	denyPermission := Permission{
		Grantee: "bob",
		Owner:   "alice",
		Object:  "network.habitat.likes",
		Effect:  "deny",
	}
	err = db.Create(&denyPermission).Error
	require.NoError(t, err)

	// Bob should still have access to posts
	hasPermission, err = store.HasPermission("bob", "alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission)

	// Bob should now be denied access to likes (deny overrides broader allow)
	hasPermission, err = store.HasPermission("bob", "alice", "network.habitat.likes", "record1")
	require.NoError(t, err)
	require.False(t, hasPermission, "deny should override broader allow")

	// Bob should also be denied access to specific like records
	hasPermission, err = store.HasPermission(
		"bob",
		"alice",
		"network.habitat.likes",
		"specific-record",
	)
	require.NoError(t, err)
	require.False(t, hasPermission, "deny should apply to all records under likes")
}

func TestPermissionStoreEmptyGrantees(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	err = store.AddReadPermission([]string{}, "alice", "network.habitat.posts")
	require.Error(t, err)
}

func TestListAllowedRecordsByGrantee(t *testing.T) {
	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	// Grant bob access to two of alice's collections
	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat.posts")
	require.NoError(t, err)
	err = store.AddReadPermission([]string{"bob"}, "alice", "network.habitat.likes")
	require.NoError(t, err)

	// Grant charlie access to one of alice's collections (should not appear)
	err = store.AddReadPermission([]string{"charlie"}, "alice", "network.habitat.posts")
	require.NoError(t, err)

	// Grant bob access to dave's collection (different owner, should not appear)
	err = store.AddReadPermission([]string{"bob"}, "dave", "network.habitat.follows")
	require.NoError(t, err)

	// List allowed records where caller=alice and grantee=bob
	uris, err := store.ListAllowedRecordsByGrantee(ctx, "alice", "bob")
	require.NoError(t, err)
	require.Len(t, uris, 2)

	expected := []habitat_syntax.HabitatURI{
		habitat_syntax.HabitatURI("habitat://alice" + "network.habitat.posts"),
		habitat_syntax.HabitatURI("habitat://alice" + "network.habitat.likes"),
	}
	require.ElementsMatch(t, expected, uris)

	// List for a grantee with no permissions — should return empty
	uris, err = store.ListAllowedRecordsByGrantee(ctx, "alice", "nobody")
	require.NoError(t, err)
	require.Empty(t, uris)
}
