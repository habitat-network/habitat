package opensocial_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/opensocial"
)

func TestScopeSetCovers(t *testing.T) {
	configure := opensocial.NewScopeSet([]string{"include:group.opensocial.configure"})

	t.Run("a permission set covers the scopes it expands to", func(t *testing.T) {
		require.True(t, configure.Covers("include:group.opensocial.configure"))
		require.True(t, configure.Covers(
			"space:group.opensocial.members?authority=self&skey=self"+
				"&collection=group.opensocial.role&action=update",
		))
	})

	t.Run("a permission set does not cover other collections or sets", func(t *testing.T) {
		require.False(t, configure.Covers(
			"space:group.opensocial.members?authority=self&skey=self"+
				"&collection=group.opensocial.membership&action=update",
		))
		require.False(t, configure.Covers("include:group.opensocial.moderate"))
	})

	t.Run("a wildcard space scope covers a narrower one", func(t *testing.T) {
		manage := opensocial.NewScopeSet([]string{"space:*?manage=create&manage=delete"})
		require.True(t, manage.Covers("space:*?manage=create"))
		require.True(t, manage.Covers("space:group.opensocial.members?manage=delete"))
		require.False(t, manage.Covers("space:*?manage=update"))
	})

	t.Run("plain scopes match exactly", func(t *testing.T) {
		read := opensocial.NewScopeSet([]string{"read"})
		require.True(t, read.Covers("read"))
		require.False(t, read.Covers("update"))
	})

	t.Run("the group DID holds every scope", func(t *testing.T) {
		require.True(t, opensocial.AllScopes().Covers("habitat:mcp=configure"))
	})
}

func TestScopeSetCanPerform(t *testing.T) {
	inviter := opensocial.NewScopeSet(opensocial.ActionScopes(opensocial.ActionInvite))
	require.True(t, inviter.CanPerform(opensocial.ActionInvite))
	require.False(t, inviter.CanPerform(opensocial.ActionEject))

	manageMembers := opensocial.NewScopeSet([]string{"include:group.opensocial.manageMembers"})
	require.True(t, manageMembers.CanPerform(opensocial.ActionInvite))
	require.True(t, manageMembers.CanPerform(opensocial.ActionEject))
	require.True(t, manageMembers.CanPerform(opensocial.ActionRoleAssign))
	require.False(t, manageMembers.CanPerform(opensocial.ActionCommunityConfigure))
}

func TestIsPermissionScope(t *testing.T) {
	require.True(t, opensocial.IsPermissionScope("rpc:group.opensocial.listSubjects"))
	require.True(t, opensocial.IsPermissionScope("include:group.opensocial.moderate"))
	require.False(t, opensocial.IsPermissionScope("atproto"))
	require.False(t, opensocial.IsPermissionScope("space:app.docs?authority=*&action=read"))
}
