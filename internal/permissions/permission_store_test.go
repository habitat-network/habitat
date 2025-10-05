package permissions

import (
	"os"
	"testing"

	fileadapter "github.com/casbin/casbin/v2/persist/file-adapter"
	"github.com/stretchr/testify/require"
)

func TestBasicPolicy1(t *testing.T) {
	a := fileadapter.NewAdapter("test_policies/test_policy_1.csv")
	ps, err := NewStore(a, false)
	require.NoError(t, err)

	// glob match on record key
	ok, err := ps.HasPermission("did:requester1", "did:owner", "app.bsky.likes", "like1")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = ps.HasPermission("did:requester1", "did:owner", "app.bsky.likes", "like2")
	require.NoError(t, err)
	require.True(t, ok)

	// glob match on nsid
	ok, err = ps.HasPermission("did:requester2", "did:owner", "app.bsky.videos", "video1")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = ps.HasPermission("did:requester2", "did:owner", "app.bsky.photos", "photo1")
	require.NoError(t, err)
	require.True(t, ok)
}

func TestAddRemovePolicies(t *testing.T) {
	inner, err := os.ReadFile("test_policies/test_add_remove_policies.csv")
	require.NoError(t, err)
	tmp, err := os.CreateTemp("test_policies", "test-tmp")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(inner)
	require.NoError(t, err)

	a := fileadapter.NewAdapter(tmp.Name())
	ps, err := NewStore(a, true)
	require.NoError(t, err)

	// inheritance
	ok, err := ps.HasPermission("did:requester1", "did:owner", "app.bsky.likes", "like1")
	require.NoError(t, err)
	require.True(t, ok)

	// Save the prev file
	prev, err := os.ReadFile(tmp.Name())
	require.NoError(t, err)

	// Add read permission
	err = ps.AddLexiconReadPermission("did:requester1", "did:owner", "app.bsky.likes-new")
	require.NoError(t, err)

	// TODO: check exact change
	next, err := os.ReadFile(tmp.Name())
	require.NoError(t, err)
	require.NotEqual(t, prev, next)

	ok, err = ps.HasPermission("did:requester1", "did:owner", "app.bsky.likes-new", "mynewlike")
	require.NoError(t, err)
	require.True(t, ok)

	// Save the prev file
	prev, err = os.ReadFile(tmp.Name())
	require.NoError(t, err)
	// Remove read permission
	err = ps.RemoveLexiconReadPermission("did:requester1", "did:owner", "app.bsky.likes-new")
	require.NoError(t, err)

	// TODO: check exact change -- check that policy was persisted
	next, err = os.ReadFile(tmp.Name())
	require.NoError(t, err)
	require.NotEqual(t, prev, next)

	ok, err = ps.HasPermission("did:requester1", "did:owner", "app.bsky.likes-new", "mynewlike")
	require.NoError(t, err)
	require.False(t, ok)

	// Remove a policy that we didn't just add -- it was in file before

	// Save the prev file
	prev, err = os.ReadFile(tmp.Name())
	require.NoError(t, err)
	// Remove read permission
	err = ps.RemoveLexiconReadPermission("did:requester1", "did:owner", "app.bsky.likes")
	require.NoError(t, err)

	// TODO: check exact change -- check that policy was persisted
	next, err = os.ReadFile(tmp.Name())
	require.NoError(t, err)
	require.NotEqual(t, prev, next)

	ok, err = ps.HasPermission("did:requester1", "did:owner", "app.bsky.likes", "myoldlike")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestList(t *testing.T) {
	a := fileadapter.NewAdapter("test_policies/test_policy_1.csv")
	ps, err := NewStore(a, false)
	require.NoError(t, err)

	perms, err := ps.ListReadPermissionsByLexicon("did:owner")
	require.NoError(t, err)

	exp := map[string][]string{
		"app.bsky":             {"did:requester2"},
		"app.bsky.likes":       {"did:requester1"},
		"app.bsky.posts":       {"did:requester2"},
		"app.bsky.posts.post1": {},
	}

	for lex, perm := range perms {
		require.Contains(t, exp, lex)
		require.ElementsMatch(t, exp[lex], perm)
	}
}
