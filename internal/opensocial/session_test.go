package opensocial_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
)

func TestStoreSessionScopes(t *testing.T) {
	ts := opensocial_testutil.NewTestStore(t)
	ctx := t.Context()
	admin := syntax.DID("did:plc:admin")
	member := syntax.DID("did:plc:member")
	orgID, err := ts.NewOrg(ctx, "acme", admin)
	require.NoError(t, err)
	group := syntax.DID(orgID)
	require.NoError(t, ts.AssignRoles(ctx, group, member, []string{opensocial.MemberRoleRkey}))

	const moderate = "include:org.atgrp.authModerate"
	const rpc = "rpc:org.atgrp.mod.querySubjects"

	t.Run("a member acts as themselves with their roles' scopes", func(t *testing.T) {
		actor, scopes, err := ts.SessionScopes(ctx, group, member, "", nil)
		require.NoError(t, err)
		require.Equal(t, member, actor)
		require.False(t, scopes.Covers(rpc))
	})

	t.Run("a session acts as its authorizer and follows their current roles", func(t *testing.T) {
		actor, scopes, err := ts.SessionScopes(ctx, group, group, admin, []string{"atproto"})
		require.NoError(t, err)
		require.Equal(t, admin, actor)
		require.True(t, scopes.Covers(rpc))

		// Demoting the authorizer narrows a session they approved earlier.
		require.NoError(t, ts.AssignRoles(ctx, group, member, []string{opensocial.AdminRoleRkey}))
		require.NoError(t, ts.AssignRoles(ctx, group, admin, []string{opensocial.MemberRoleRkey}))
		_, scopes, err = ts.SessionScopes(ctx, group, group, admin, []string{"atproto"})
		require.NoError(t, err)
		require.False(t, scopes.Covers(rpc))
		require.NoError(t, ts.AssignRoles(ctx, group, admin, []string{opensocial.AdminRoleRkey}))
	})

	t.Run("a token that asks for specific scopes narrows the authorizer", func(t *testing.T) {
		_, scopes, err := ts.SessionScopes(ctx, group, group, admin, []string{"atproto", moderate})
		require.NoError(t, err)
		require.True(t, scopes.Covers(rpc))
		require.False(t, scopes.Covers("rpc:org.atgrp.denyJoinRequest"), "not in the token")
		require.False(t, scopes.Covers("space:*?manage=create"))
	})

	t.Run("a session issued without an authorizer acts as the group", func(t *testing.T) {
		actor, scopes, err := ts.SessionScopes(ctx, group, group, "", []string{"atproto"})
		require.NoError(t, err)
		require.Equal(t, group, actor)
		require.True(t, scopes.Covers("space:*?manage=create"))
	})

	t.Run("an ejected authorizer leaves the session with nothing", func(t *testing.T) {
		require.NoError(t, ts.AssignRoles(ctx, group, member, []string{opensocial.AdminRoleRkey}))
		require.NoError(t, ts.EjectMember(ctx, group, admin))
		_, scopes, err := ts.SessionScopes(ctx, group, group, admin, nil)
		require.NoError(t, err)
		require.False(t, scopes.Covers(rpc))
	})
}
