package permissions

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/node"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type mockNode struct {
	servedDIDs map[string]bool
}

var _ node.Node = &mockNode{}

func (m *mockNode) ServesDID(_ context.Context, did syntax.DID) (bool, error) {
	return m.servedDIDs[did.String()], nil
}

func (m *mockNode) SendXRPC(_ context.Context, _ syntax.DID, _ syntax.DID, _ *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestStoreBasicPermissions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db, node.New("test", "test", nil, nil))
	require.NoError(t, err)

	// Test: Owner always has permission
	HasPermission, err := store.HasPermission(t.Context(), "did:example:alice", "did:example:alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, HasPermission, "owner should always have permission")

	// Test: Non-owner without permission should be denied
	HasPermission, err = store.HasPermission(t.Context(), "did:example:bob", "did:example:alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.False(t, HasPermission, "non-owner without permission should be denied")

	// Test: Grant lexicon-level permission
	_, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)

	// Test: Bob should now have permission to all posts
	HasPermission, err = store.HasPermission(t.Context(), "did:example:bob", "did:example:alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, HasPermission, "bob should have permission after grant")

	HasPermission, err = store.HasPermission(t.Context(), "did:example:bob", "did:example:alice", "network.habitat.posts", "record2")
	require.NoError(t, err)
	require.True(t, HasPermission, "bob should have permission to all records in the lexicon")

	// Test: Bob should not have permission to other lexicons
	HasPermission, err = store.HasPermission(t.Context(), "did:example:bob", "did:example:alice", "network.habitat.likes", "record1")
	require.NoError(t, err)
	require.False(t, HasPermission, "bob should not have permission to other lexicons")

	// Test: Remove permission
	err = store.RemovePermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)

	HasPermission, err = store.HasPermission(t.Context(), "did:example:bob", "did:example:alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.False(t, HasPermission, "bob should not have permission after removal")
}

func TestStoreMultipleGrantees(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db, node.New("test", "test", nil, nil))
	require.NoError(t, err)

	// Grant permissions to multiple users
	grantees := []Grantee{DIDGrantee("did:example:bob"), DIDGrantee("did:example:charlie")}
	granted, err := store.AddPermissions(grantees, "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)
	require.Len(t, granted, 2)

	slices.SortFunc(granted, func(a, b Grantee) int {
		return strings.Compare(a.String(), b.String())
	})
	slices.SortFunc(grantees, func(a, b Grantee) int {
		return strings.Compare(a.String(), b.String())
	})
	require.Equal(t, granted, grantees)

	granted, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)
	require.Len(t, granted, 0)

	granted, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "post-1")
	require.NoError(t, err)
	require.Len(t, granted, 0)

	granted, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.likes", "like-1")
	require.NoError(t, err)
	require.Len(t, granted, 1)
	require.Equal(t, "did:example:bob", granted[0].String())

	granted, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.likes", "")
	require.NoError(t, err)
	require.Len(t, granted, 1)

	// List permissions by lexicon
	permissions, err := store.listPermissions(DIDGrantee(""), []syntax.DID{"did:example:alice"}, "", "")
	require.NoError(t, err)
	// Alice gave three permission grants
	require.Len(t, permissions, 3)
}

func TestStoreListByGrantee(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db, node.New("test", "test", nil, nil))
	require.NoError(t, err)

	// Grant bob access to network.habitat.posts
	_, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)

	// List bob's permissions for network.habitat.posts
	permissions, err := store.listPermissions(DIDGrantee("did:example:bob"), nil, "network.habitat.posts", "")
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
	permissions, err = store.listPermissions(DIDGrantee("did:example:charlie"), nil, "network.habitat.posts", "")
	require.NoError(t, err)
	require.Len(t, permissions, 1)
	require.Equal(t, permissions[0].Owner.String(), "did:example:charlie")
	require.Equal(t, permissions[0].Grantee.String(), "did:example:charlie")
	require.Equal(t, permissions[0].Effect, Allow)
}

func TestStoreEmptyRecordKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db, node.New("test", "test", nil, nil))
	require.NoError(t, err)

	// Grant permission
	_, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)

	// Check permission with empty record key (should check NSID-level permission)
	HasPermission, err := store.HasPermission(t.Context(), "did:example:bob", "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)
	require.True(t, HasPermission, "should have permission to NSID when record key is empty")
}

func TestStoreMultipleOwners(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db, node.New("test", "test", nil, nil))
	require.NoError(t, err)

	// Grant bob access to alice's posts
	_, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)

	// Grant bob access to charlie's likes
	_, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:charlie", "network.habitat.likes", "")
	require.NoError(t, err)

	// Bob should have access to alice's posts
	HasPermission, err := store.HasPermission(t.Context(), "did:example:bob", "did:example:alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, HasPermission)

	// Bob should not have access to alice's likes
	HasPermission, err = store.HasPermission(t.Context(), "did:example:bob", "did:example:alice", "network.habitat.likes", "record1")
	require.NoError(t, err)
	require.False(t, HasPermission)

	// Bob should have access to charlie's likes
	HasPermission, err = store.HasPermission(t.Context(), "did:example:bob", "did:example:charlie", "network.habitat.likes", "record1")
	require.NoError(t, err)
	require.True(t, HasPermission)

	// List alice's permissions
	permissions, err := store.listPermissions(DIDGrantee(""), []syntax.DID{"did:example:alice"}, "", "")
	require.NoError(t, err)
	require.Len(t, permissions, 1)
	require.Equal(t, Permission{Grantee: DIDGrantee("did:example:bob"), Owner: "did:example:alice", Collection: "network.habitat.posts", Rkey: "", Effect: Allow}, permissions[0])

	// List charlie's permissions
	permissions, err = store.listPermissions(DIDGrantee(""), []syntax.DID{"did:example:charlie"}, "", "")
	require.NoError(t, err)
	require.Len(t, permissions, 1)
	require.Equal(t, Permission{Grantee: DIDGrantee("did:example:bob"), Owner: "did:example:charlie", Collection: "network.habitat.likes", Rkey: "", Effect: Allow}, permissions[0])
}

func TestStoreDenyOverridesAllow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db, node.New("test", "test", nil, nil))
	require.NoError(t, err)

	// Grant bob broad access to network.habitat
	_, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)

	err = store.RemovePermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "record1")
	require.NoError(t, err)

	// Bob should not have access to the denied post
	HasPermission, err := store.HasPermission(t.Context(), "did:example:bob", "did:example:alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.False(t, HasPermission)

	// Bob should have access to other posts
	HasPermission, err = store.HasPermission(t.Context(), "did:example:bob", "did:example:alice", "network.habitat.posts", "record2")
	require.NoError(t, err)
	require.True(t, HasPermission)
}

func TestHasPermissionViaClique(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// The node serves did:example:alice, making her clique a local clique.
	n := &mockNode{servedDIDs: map[string]bool{"did:example:alice": true}}
	store, err := NewStore(db, n)
	require.NoError(t, err)

	clique := CliqueGrantee("habitat://did:example:alice/network.habitat.clique/my-clique")

	// Alice grants her clique access to her posts.
	_, err = store.AddPermissions([]Grantee{clique}, "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)

	// Bob is a member of Alice's clique.
	grantees, err := store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.clique", "my-clique")
	require.NoError(t, err)
	require.Len(t, grantees, 1)
	require.Equal(t, grantees[0].String(), "did:example:bob")

	// Bob should have permission to Alice's posts via the clique.
	hasPermission, err := store.HasPermission(t.Context(), "did:example:bob", "did:example:alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.True(t, hasPermission, "bob should have permission via clique membership")

	// Charlie is not a member of the clique and should not have permission.
	hasPermission, err = store.HasPermission(t.Context(), "did:example:charlie", "did:example:alice", "network.habitat.posts", "record1")
	require.NoError(t, err)
	require.False(t, hasPermission, "charlie should not have permission without clique membership")
}

func TestResolvePermissionsForCollection(t *testing.T) {
	t.Run("direct permission", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)

		store, err := NewStore(db, node.New("test", "test", nil, nil))
		require.NoError(t, err)

		_, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "")
		require.NoError(t, err)

		perms, err := store.ResolvePermissionsForCollection(t.Context(), "did:example:bob", "network.habitat.posts", nil)
		require.NoError(t, err)
		// Should include alice's direct grant and bob's own self-permission
		require.Len(t, perms, 2)
		var alicePerm *Permission
		for i := range perms {
			if perms[i].Owner == "did:example:alice" {
				alicePerm = &perms[i]
			}
		}
		require.NotNil(t, alicePerm)
		require.Equal(t, DIDGrantee("did:example:bob"), alicePerm.Grantee)
		require.Equal(t, Allow, alicePerm.Effect)
	})

	t.Run("no external permissions returns only self", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)

		store, err := NewStore(db, node.New("test", "test", nil, nil))
		require.NoError(t, err)

		perms, err := store.ResolvePermissionsForCollection(t.Context(), "did:example:charlie", "network.habitat.posts", nil)
		require.NoError(t, err)
		require.Len(t, perms, 1)
		require.Equal(t, DIDGrantee("did:example:charlie"), perms[0].Grantee)
		require.Equal(t, syntax.DID("did:example:charlie"), perms[0].Owner)
		require.Equal(t, Allow, perms[0].Effect)
	})

	t.Run("clique permission resolved for member", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)

		// Node serves alice, so her clique is resolved locally
		n := &mockNode{servedDIDs: map[string]bool{"did:example:alice": true}}
		store, err := NewStore(db, n)
		require.NoError(t, err)

		clique := CliqueGrantee("habitat://did:example:alice/network.habitat.clique/friends")

		// Alice grants her clique access to her posts
		_, err = store.AddPermissions([]Grantee{clique}, "did:example:alice", "network.habitat.posts", "")
		require.NoError(t, err)

		// Bob is a member of Alice's clique
		_, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.clique", "friends")
		require.NoError(t, err)

		perms, err := store.ResolvePermissionsForCollection(t.Context(), "did:example:bob", "network.habitat.posts", nil)
		require.NoError(t, err)
		// Should include: clique grant resolved to bob + bob's own self-permission
		require.Len(t, perms, 2)
		var alicePerm *Permission
		for i := range perms {
			if perms[i].Owner == "did:example:alice" {
				alicePerm = &perms[i]
			}
		}
		require.NotNil(t, alicePerm, "should include permission from alice resolved via clique")
		require.Equal(t, DIDGrantee("did:example:bob"), alicePerm.Grantee, "clique grantee should be resolved to bob's DID")
		require.Equal(t, Allow, alicePerm.Effect)
	})

	t.Run("clique permission excluded for non-member", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)

		// Node serves alice, so her clique is resolved locally
		n := &mockNode{servedDIDs: map[string]bool{"did:example:alice": true}}
		store, err := NewStore(db, n)
		require.NoError(t, err)

		clique := CliqueGrantee("habitat://did:example:alice/network.habitat.clique/friends")

		// Alice grants her clique access to her posts
		_, err = store.AddPermissions([]Grantee{clique}, "did:example:alice", "network.habitat.posts", "")
		require.NoError(t, err)

		// Bob is a member but charlie is not
		_, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.clique", "friends")
		require.NoError(t, err)

		// Charlie is not in the clique and should only see her own self-permission
		perms, err := store.ResolvePermissionsForCollection(t.Context(), "did:example:charlie", "network.habitat.posts", nil)
		require.NoError(t, err)
		require.Len(t, perms, 1)
		require.Equal(t, DIDGrantee("did:example:charlie"), perms[0].Grantee)
		require.Equal(t, syntax.DID("did:example:charlie"), perms[0].Owner)
	})
}

func TestListAllowedGranteesForRecord(t *testing.T) {
	t.Run("basic: grantee with allow permission is returned", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		store, err := NewStore(db, node.New("test", "test", nil, nil))
		require.NoError(t, err)

		_, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "record1")
		require.NoError(t, err)

		grants, err := store.ListAllowedGranteesForRecord(t.Context(), "did:example:alice", "network.habitat.posts", "record1")
		require.NoError(t, err)
		require.Len(t, grants, 1)
		require.Equal(t, DIDGrantee("did:example:bob"), grants[0])
	})

	t.Run("no permission returns empty", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		store, err := NewStore(db, node.New("test", "test", nil, nil))
		require.NoError(t, err)

		perms, err := store.ListAllowedGranteesForRecord(t.Context(), "did:example:alice", "network.habitat.posts", "record1")
		require.NoError(t, err)
		require.Empty(t, perms)
	})

	t.Run("deny permission is filtered out", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		store, err := NewStore(db, node.New("test", "test", nil, nil))
		require.NoError(t, err)

		// Grant bob access at the collection level
		_, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "")
		require.NoError(t, err)

		// Add a deny directly on the record without any allow
		err = store.RemovePermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "record1")
		require.NoError(t, err)

		perms, err := store.ListAllowedGranteesForRecord(t.Context(), "did:example:alice", "network.habitat.posts", "record1")
		require.NoError(t, err)
		require.Empty(t, perms)
	})

	t.Run("collection-level allow is included for specific record with no record-level deny", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		store, err := NewStore(db, node.New("test", "test", nil, nil))
		require.NoError(t, err)

		// Grant bob access at the collection level
		_, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "")
		require.NoError(t, err)

		// Add a deny on a different record to confirm it doesn't pollute results for record1
		err = store.RemovePermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "record2")
		require.NoError(t, err)

		// record1 has no record-level deny, so the collection-level allow should appear
		grants, err := store.ListAllowedGranteesForRecord(t.Context(), "did:example:alice", "network.habitat.posts", "record1")
		require.NoError(t, err)
		require.Len(t, grants, 1)
		require.Equal(t, DIDGrantee("did:example:bob"), grants[0])

		// record2 has a deny, which should be filtered; the collection-level allow still appears
		grants, err = store.ListAllowedGranteesForRecord(t.Context(), "did:example:alice", "network.habitat.posts", "record2")
		require.NoError(t, err)
		require.Len(t, grants, 0)
	})
}

func TestAddReadPermission_EmptyCollection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db, node.New("test", "test", nil, nil))
	require.NoError(t, err)

	_, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "", "")
	require.Error(t, err)
}

func TestListReadPermissionsByGrantee_NoRedundant(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewStore(db, node.New("test", "test", nil, nil))
	require.NoError(t, err)

	_, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "record-1")
	require.NoError(t, err)

	// Should not return multiple permissions for the same record
	_, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "record-1")
	require.NoError(t, err)

	perms, err := store.listPermissions(DIDGrantee("did:example:bob"), nil, "", "")
	require.NoError(t, err)
	require.Len(t, perms, 1 /* has direct permission on record */)

	// Should only return the most powerful permission
	_, err = store.AddPermissions([]Grantee{DIDGrantee("did:example:bob")}, "did:example:alice", "network.habitat.posts", "")
	require.NoError(t, err)

	perms, err = store.listPermissions(DIDGrantee("did:example:bob"), nil, "", "")
	require.NoError(t, err)
	require.Len(t, perms, 1 /* no collection specified so only returns direct permissions */)

	// should not add unnecessary permissions
	_, err = store.AddPermissions(
		[]Grantee{DIDGrantee("did:example:bob"), DIDGrantee("did:example:charlie")},
		"did:example:alice",
		"network.habitat.posts",
		"record-1",
	)
	require.NoError(t, err)

	perms, err = store.listPermissions(DIDGrantee("did:example:bob"), nil, "", "")
	require.NoError(t, err)
	require.Len(t, perms, 1 /* has direct permission on record */)
}
