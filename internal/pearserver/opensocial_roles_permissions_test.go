package pearserver_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bluesky-social/indigo/atproto/syntax"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
)

func TestServer_PutRole(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("requires community.configure", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var out struct{}
		code := client.Procedure(
			ts.Server.PutRole,
			opensocial_api.CommunityOpensocialPutRoleInput{
				Org: orgDID, Role: "moderator", Name: "Moderator",
			},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("admin creates a role", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var out struct{}
		code := client.Procedure(
			ts.Server.PutRole,
			opensocial_api.CommunityOpensocialPutRoleInput{
				Org: orgDID, Role: "moderator", Name: "Moderator",
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
	})
}

func TestServer_DeleteRole(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("cannot delete builtin role", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var out struct{}
		code := client.Procedure(
			ts.Server.DeleteRole,
			opensocial_api.CommunityOpensocialDeleteRoleInput{
				Org: orgDID, Role: opensocial.AdminRoleRkey,
			},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("admin deletes a role", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		require.NoError(
			t,
			ts.OpenSocialStore.PutRole(
				t.Context(),
				syntax.DID(orgDID),
				"moderator",
				"Moderator",
				"",
			),
		)

		var out struct{}
		code := client.Procedure(
			ts.Server.DeleteRole,
			opensocial_api.CommunityOpensocialDeleteRoleInput{
				Org: orgDID, Role: "moderator",
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
	})
}

func TestServer_UpdatePermissions(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("requires community.configure", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var out struct{}
		code := client.Procedure(
			ts.Server.UpdatePermissions,
			opensocial_api.CommunityOpensocialUpdatePermissionsInput{Org: orgDID},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("admin rebinds actions to roles", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var out struct{}
		code := client.Procedure(
			ts.Server.UpdatePermissions,
			opensocial_api.CommunityOpensocialUpdatePermissionsInput{
				Org: orgDID,
				Bindings: []opensocial_api.CommunityOpensocialPermissionsActionBinding{
					{
						Action: string(opensocial.ActionCommunityConfigure),
						Roles:  []string{opensocial.AdminRoleRkey},
					},
				},
				Assignable: []opensocial_api.CommunityOpensocialPermissionsAssignableBinding{
					{Role: opensocial.AdminRoleRkey, Roles: []string{opensocial.MemberRoleRkey}},
				},
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)

		ok, err := ts.OpenSocialStore.CheckAction(
			t.Context(), syntax.DID(orgDID), admin, opensocial.ActionCommunityConfigure,
		)
		require.NoError(t, err)
		require.True(t, ok)
	})
}

func TestServer_AssignRoles(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("bounded by the caller's assignable roles", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		require.NoError(
			t, ts.OpenSocialStore.AssignRoles(
				t.Context(), syntax.DID(orgDID), alice, []string{opensocial.MemberRoleRkey},
			),
		)
		// Narrow the admin's assignable set to exclude the admin role itself.
		require.NoError(t, ts.OpenSocialStore.PutPermissions(
			t.Context(), syntax.DID(orgDID),
			[]opensocial_api.CommunityOpensocialPermissionsActionBinding{
				{
					Action: string(opensocial.ActionRoleAssign),
					Roles:  []string{opensocial.AdminRoleRkey},
				},
			},
			[]opensocial_api.CommunityOpensocialPermissionsAssignableBinding{
				{Role: opensocial.AdminRoleRkey, Roles: []string{opensocial.MemberRoleRkey}},
			},
		))

		var out struct{}
		code := client.Procedure(
			ts.Server.AssignRoles,
			opensocial_api.CommunityOpensocialAssignRolesInput{
				Org: orgDID, Member: alice.String(), Roles: []string{opensocial.AdminRoleRkey},
			},
			&out,
		)
		require.Equal(t, http.StatusForbidden, code)
	})

	t.Run("admin assigns a member role within bounds", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		require.NoError(
			t, ts.OpenSocialStore.AssignRoles(
				t.Context(), syntax.DID(orgDID), alice, []string{opensocial.MemberRoleRkey},
			),
		)

		var out struct{}
		code := client.Procedure(
			ts.Server.AssignRoles,
			opensocial_api.CommunityOpensocialAssignRolesInput{
				Org: orgDID, Member: alice.String(), Roles: []string{opensocial.AdminRoleRkey},
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)

		roles, err := ts.OpenSocialStore.GetUserRoles(t.Context(), syntax.DID(orgDID), alice)
		require.NoError(t, err)
		require.Equal(t, []string{opensocial.AdminRoleRkey}, roles)
	})
}

func TestServer_EjectMember(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("admin ejects a member", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		require.NoError(
			t, ts.OpenSocialStore.AssignRoles(
				t.Context(), syntax.DID(orgDID), alice, []string{opensocial.MemberRoleRkey},
			),
		)

		var out struct{}
		code := client.Procedure(
			ts.Server.EjectMember,
			opensocial_api.CommunityOpensocialEjectMemberInput{
				Org: orgDID, Member: alice.String(),
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)

		roles, err := ts.OpenSocialStore.GetUserRoles(t.Context(), syntax.DID(orgDID), alice)
		require.NoError(t, err)
		require.Empty(t, roles)
	})

	t.Run("non-admin cannot eject", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var out struct{}
		code := client.Procedure(
			ts.Server.EjectMember,
			opensocial_api.CommunityOpensocialEjectMemberInput{
				Org: orgDID, Member: admin.String(),
			},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})
}
