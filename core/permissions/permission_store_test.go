package permissions

import (
	"testing"

	fileadapter "github.com/casbin/casbin/v2/persist/file-adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPermissionStore_TestPolicy1(t *testing.T) {
	a := fileadapter.NewAdapter("test_policies/test_policy_1.csv")
	ps, err := NewPermissionStore(a)
	require.NoError(t, err)

	// inheritance
	ok, err := ps.HasPermission("did:1", "app.bsky.posts", "post1", true)
	assert.NoError(t, err)
	assert.True(t, ok)
	ok, err = ps.HasPermission("did:1", "app.bsky.posts", "post2", true)
	assert.NoError(t, err)
	assert.False(t, ok)

	// glob match on record key
	ok, err = ps.HasPermission("did:1", "app.bsky.likes", "like1", false)
	assert.NoError(t, err)
	assert.True(t, ok)
	ok, err = ps.HasPermission("did:1", "app.bsky.likes", "like2", false)
	assert.NoError(t, err)
	assert.True(t, ok)

	// glob match on nsid
	ok, err = ps.HasPermission("did:2", "app.bsky.videos", "video1", false)
	assert.NoError(t, err)
	assert.True(t, ok)
	ok, err = ps.HasPermission("did:2", "app.bsky.photos", "photo1", false)
	assert.NoError(t, err)
	assert.True(t, ok)

	// action match
	ok, err = ps.HasPermission("did:1", "app.bsky.likes", "like1", true)
	assert.NoError(t, err)
	assert.False(t, ok)
}
