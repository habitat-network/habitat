package permissions

import (
	"fmt"
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

func TestStoreListFullAccessOwnersForGranteeCollection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	collection := "network.habitat.posts"
	grantee := "bob"

	t.Run("returns empty when no permissions exist", func(t *testing.T) {
		owners, err := store.ListFullAccessOwnersForGranteeCollection(grantee, collection)
		require.NoError(t, err)
		require.Empty(t, owners)
	})

	t.Run("returns owners with exact collection match", func(t *testing.T) {
		require.NoError(t, store.AddReadPermission([]string{grantee}, "alice", collection))
		require.NoError(t, store.AddReadPermission([]string{grantee}, "charlie", collection))

		owners, err := store.ListFullAccessOwnersForGranteeCollection(grantee, collection)
		require.NoError(t, err)
		require.Len(t, owners, 2)
		require.Contains(t, owners, "alice")
		require.Contains(t, owners, "charlie")
	})

	t.Run("returns owners with parent wildcard match", func(t *testing.T) {
		// Create wildcard permission directly (stored as "network.habitat.*")
		wildcardPermission := Permission{
			Grantee: grantee,
			Owner:   "david",
			Object:  "network.habitat.*",
			Effect:  "allow",
		}
		require.NoError(t, db.Create(&wildcardPermission).Error)

		owners, err := store.ListFullAccessOwnersForGranteeCollection(grantee, collection)
		require.NoError(t, err)
		require.Contains(t, owners, "david", "parent wildcard should match child collection")
	})

	t.Run("does not return owners with specific record permissions", func(t *testing.T) {
		// Grant specific record permission (not full collection)
		require.NoError(t, store.AddReadPermission([]string{grantee}, "eve", fmt.Sprintf("%s.record1", collection)))

		owners, err := store.ListFullAccessOwnersForGranteeCollection(grantee, collection)
		require.NoError(t, err)
		require.NotContains(t, owners, "eve", "specific record permission should not grant full access")
	})

	t.Run("does not return owners for different collections", func(t *testing.T) {
		otherCollection := "network.habitat.likes"
		require.NoError(t, store.AddReadPermission([]string{grantee}, "frank", otherCollection))

		owners, err := store.ListFullAccessOwnersForGranteeCollection(grantee, collection)
		require.NoError(t, err)
		require.NotContains(t, owners, "frank", "permission for different collection should not match")
	})

	t.Run("filters by grantee", func(t *testing.T) {
		otherGrantee := "charlie"
		require.NoError(t, store.AddReadPermission([]string{otherGrantee}, "grace", collection))

		owners, err := store.ListFullAccessOwnersForGranteeCollection(grantee, collection)
		require.NoError(t, err)
		require.NotContains(t, owners, "grace", "permissions for other grantee should not be included")
	})
}

func TestStoreListSpecificRecordsForGranteeCollection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db)
	require.NoError(t, err)

	collection := "network.habitat.posts"
	grantee := "bob"

	t.Run("returns empty when no permissions exist", func(t *testing.T) {
		records, err := store.ListSpecificRecordsForGranteeCollection(grantee, collection)
		require.NoError(t, err)
		require.Empty(t, records)
	})

	t.Run("returns specific record permissions", func(t *testing.T) {
		require.NoError(t, store.AddReadPermission([]string{grantee}, "alice", fmt.Sprintf("%s.record1", collection)))
		require.NoError(t, store.AddReadPermission([]string{grantee}, "alice", fmt.Sprintf("%s.record2", collection)))
		require.NoError(t, store.AddReadPermission([]string{grantee}, "charlie", fmt.Sprintf("%s.record3", collection)))

		records, err := store.ListSpecificRecordsForGranteeCollection(grantee, collection)
		require.NoError(t, err)
		require.Len(t, records, 3)

		// Check alice's records
		aliceRecords := []RecordPermission{}
		for _, r := range records {
			if r.Owner == "alice" {
				aliceRecords = append(aliceRecords, r)
			}
		}
		require.Len(t, aliceRecords, 2)
		require.Contains(t, []string{aliceRecords[0].Rkey, aliceRecords[1].Rkey}, "record1")
		require.Contains(t, []string{aliceRecords[0].Rkey, aliceRecords[1].Rkey}, "record2")

		// Check charlie's record
		charlieRecords := []RecordPermission{}
		for _, r := range records {
			if r.Owner == "charlie" {
				charlieRecords = append(charlieRecords, r)
			}
		}
		require.Len(t, charlieRecords, 1)
		require.Equal(t, "record3", charlieRecords[0].Rkey)
	})

	t.Run("does not return full collection permissions", func(t *testing.T) {
		// Grant full collection permission (not specific record)
		require.NoError(t, store.AddReadPermission([]string{grantee}, "david", collection))

		records, err := store.ListSpecificRecordsForGranteeCollection(grantee, collection)
		require.NoError(t, err)
		// Should not include david's full collection permission
		for _, r := range records {
			require.NotEqual(t, "david", r.Owner, "full collection permission should not be in specific records list")
		}
	})

	t.Run("does not return permissions for different collections", func(t *testing.T) {
		otherCollection := "network.habitat.likes"
		require.NoError(t, store.AddReadPermission([]string{grantee}, "eve", fmt.Sprintf("%s.record1", otherCollection)))

		records, err := store.ListSpecificRecordsForGranteeCollection(grantee, collection)
		require.NoError(t, err)
		// Should not include records from other collection
		for _, r := range records {
			require.NotEqual(t, "eve", r.Owner, "permissions for different collection should not be included")
		}
	})

	t.Run("filters by grantee", func(t *testing.T) {
		otherGrantee := "charlie"
		require.NoError(t, store.AddReadPermission([]string{otherGrantee}, "frank", fmt.Sprintf("%s.record4", collection)))

		records, err := store.ListSpecificRecordsForGranteeCollection(grantee, collection)
		require.NoError(t, err)
		// Should not include permissions for other grantee
		for _, r := range records {
			require.NotEqual(t, "frank", r.Owner, "permissions for other grantee should not be included")
		}
	})

	t.Run("handles parent wildcard permissions correctly", func(t *testing.T) {
		// Parent wildcard should not be returned as specific record
		require.NoError(t, store.AddReadPermission([]string{grantee}, "grace", "network.habitat"))

		records, err := store.ListSpecificRecordsForGranteeCollection(grantee, collection)
		require.NoError(t, err)
		// Should not include parent wildcard as specific record
		for _, r := range records {
			require.NotEqual(t, "grace", r.Owner, "parent wildcard should not be in specific records list")
		}
	})
}
