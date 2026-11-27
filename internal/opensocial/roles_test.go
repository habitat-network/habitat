package opensocial_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
)

const (
	rolesAdmin  = syntax.DID("did:plc:rolesadmin")
	rolesMember = syntax.DID("did:plc:rolesmember")
	rolesEditor = syntax.DID("did:plc:roleseditor")
)

// newRolesOrg mints an org with an admin, a plain member, and an editor who
// holds only the invite action and may assign only the viewer role.
func newRolesOrg(t *testing.T) (*opensocial_testutil.TestStore, syntax.DID) {
	t.Helper()
	s := newTestStore(t)
	orgStr, err := s.NewOrg(t.Context(), "roles", rolesAdmin)
	require.NoError(t, err)
	org := syntax.DID(orgStr)
	require.NoError(t, s.PutRole(t.Context(), org, "viewer", opensocial.Role{DisplayName: "Viewer"}))
	require.NoError(t, s.PutRole(t.Context(), org, "editor", opensocial.Role{
		DisplayName: "Editor",
		Permissions: []string{opensocial.ActionScope(opensocial.ActionInvite)},
		CanAssign:   []string{"viewer"},
	}))
	require.NoError(t, s.AssignRoles(t.Context(), org, rolesMember, []string{"member"}))
	require.NoError(t, s.AssignRoles(t.Context(), org, rolesEditor, []string{"editor"}))
	return s, org
}

func TestRolesBootstrap(t *testing.T) {
	s, org := newRolesOrg(t)

	roles, err := s.GetRoles(t.Context(), org)
	require.NoError(t, err)
	require.Equal(t, []string{opensocial.AssignAll}, roles["admin"].CanAssign)
	require.True(t, roles["member"].Default)

	adminScopes, err := s.UserScopes(t.Context(), org, rolesAdmin)
	require.NoError(t, err)
	for _, action := range opensocial.AllActions {
		require.True(t, adminScopes.CanPerform(action), action)
	}
	memberScopes, err := s.UserScopes(t.Context(), org, rolesMember)
	require.NoError(t, err)
	require.False(t, memberScopes.CanPerform(opensocial.ActionInvite))
	groupScopes, err := s.UserScopes(t.Context(), org, org)
	require.NoError(t, err)
	require.True(t, groupScopes.Covers("rpc:anything"))

	ok, err := s.CheckAction(t.Context(), org, rolesEditor, opensocial.ActionInvite)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = s.CheckAction(t.Context(), org, rolesEditor, opensocial.ActionEject)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestAssignableRoles(t *testing.T) {
	s, org := newRolesOrg(t)

	adminSet, err := s.AssignableRoles(t.Context(), org, rolesAdmin)
	require.NoError(t, err)
	require.True(t, adminSet.HasAll())
	require.True(t, adminSet.Has("anything"))
	editorSet, err := s.AssignableRoles(t.Context(), org, rolesEditor)
	require.NoError(t, err)
	require.Equal(t, []string{"viewer"}, editorSet.Roles())
	require.False(t, editorSet.Has("admin"))
	groupSet, err := s.AssignableRoles(t.Context(), org, org)
	require.NoError(t, err)
	require.True(t, groupSet.HasAll())
	nobody, err := s.AssignableRoles(t.Context(), org, "did:plc:stranger")
	require.NoError(t, err)
	require.Empty(t, nobody.Roles())

	for _, tc := range []struct {
		name            string
		actor           syntax.DID
		current, target []string
		want            bool
	}{
		{"editor grants viewer", rolesEditor, []string{"member"}, []string{"member", "viewer"}, true},
		{"editor revokes viewer", rolesEditor, []string{"viewer"}, nil, true},
		{"editor cannot grant admin", rolesEditor, nil, []string{"admin"}, false},
		{"editor cannot revoke admin", rolesEditor, []string{"admin"}, nil, false},
		{"admin grants anything", rolesAdmin, nil, []string{"admin", "editor"}, true},
		{"member cannot assign", rolesMember, nil, []string{"viewer"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := s.CanAssignRoles(t.Context(), org, tc.actor, tc.current, tc.target)
			require.NoError(t, err)
			require.Equal(t, tc.want, ok)
		})
	}
}

func TestUpdateRoleEscalation(t *testing.T) {
	s, org := newRolesOrg(t)
	invite := opensocial.ActionScope(opensocial.ActionInvite)

	// The editor may give the viewer role a permission they hold themselves.
	require.NoError(t, s.UpdateRole(t.Context(), org, rolesEditor, "viewer", opensocial.Role{
		DisplayName: "Viewer", Permissions: []string{invite},
	}))

	for _, tc := range []struct {
		name string
		rkey syntax.RecordKey
		role opensocial.Role
	}{
		{"role not assignable", "admin", opensocial.Role{DisplayName: "Admin"}},
		{"permission not held", "viewer", opensocial.Role{
			Permissions: []string{invite, "include:org.atgrp.authManageAdmin"},
		}},
		{"canAssign not held", "viewer", opensocial.Role{
			Permissions: []string{invite}, CanAssign: []string{"admin"},
		}},
		{"wildcard canAssign", "viewer", opensocial.Role{
			Permissions: []string{invite}, CanAssign: []string{opensocial.AssignAll},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := s.UpdateRole(t.Context(), org, rolesEditor, tc.rkey, tc.role)
			require.ErrorIs(t, err, opensocial.ErrEscalation)
		})
	}

	// The group and holders of `*` write anything, including `*` itself.
	require.NoError(t, s.UpdateRole(t.Context(), org, rolesAdmin, "viewer", opensocial.Role{
		DisplayName: "Viewer", CanAssign: []string{opensocial.AssignAll},
	}))
	require.NoError(t, s.UpdateRole(t.Context(), org, org, "viewer", opensocial.Role{
		DisplayName: "Viewer",
	}))
	require.ErrorIs(
		t,
		s.UpdateRole(t.Context(), org, rolesAdmin, "Bad_ID", opensocial.Role{}),
		opensocial.ErrInvalidRoleID,
	)
}

func TestRoleProfileAndRemoval(t *testing.T) {
	s, org := newRolesOrg(t)

	require.NoError(t, s.SetRoleProfile(t.Context(), org, rolesEditor, "viewer", "Watcher", "reads"))
	roles, err := s.GetRoles(t.Context(), org)
	require.NoError(t, err)
	require.Equal(t, "Watcher", roles["viewer"].DisplayName)
	// Renaming keeps the grants the role already had.
	require.NoError(t, s.SetRoleProfile(t.Context(), org, rolesAdmin, "editor", "Writer", ""))
	roles, err = s.GetRoles(t.Context(), org)
	require.NoError(t, err)
	require.Equal(t, []string{opensocial.ActionScope(opensocial.ActionInvite)}, roles["editor"].Permissions)

	require.ErrorIs(
		t, s.RemoveRole(t.Context(), org, rolesEditor, "editor"), opensocial.ErrEscalation,
	)
	require.ErrorIs(
		t, s.RemoveRole(t.Context(), org, rolesAdmin, "admin"), opensocial.ErrCannotDeleteBuiltinRole,
	)
	require.ErrorIs(
		t, s.RemoveRole(t.Context(), org, rolesAdmin, "ghost"), opensocial.ErrRoleNotFound,
	)
	require.NoError(t, s.RemoveRole(t.Context(), org, rolesEditor, "viewer"))
	roles, err = s.GetRoles(t.Context(), org)
	require.NoError(t, err)
	require.NotContains(t, roles, "viewer")
}

func TestEjectMember(t *testing.T) {
	s, org := newRolesOrg(t)

	require.NoError(t, s.EjectMember(t.Context(), org, rolesMember))
	roles, err := s.GetUserRoles(t.Context(), org, rolesMember)
	require.NoError(t, err)
	require.Empty(t, roles)
	require.ErrorIs(t, s.EjectMember(t.Context(), org, rolesMember), opensocial.ErrMemberNotFound)
}

func TestUpdatePermissionsRebindsActions(t *testing.T) {
	s, org := newRolesOrg(t)
	bindings := []habitat.NetworkHabitatOpensocialDefsActionBinding{
		{Action: string(opensocial.ActionInvite), Roles: []string{"member", "editor"}},
	}
	assignable := []habitat.NetworkHabitatOpensocialDefsAssignableBinding{
		{Role: "member", Roles: []string{"viewer"}},
		{Role: "editor", Roles: []string{"viewer"}},
		{Role: "admin", Roles: []string{opensocial.AssignAll}},
	}
	require.NoError(t, s.UpdatePermissions(t.Context(), org, rolesAdmin, bindings, assignable))

	ok, err := s.CheckAction(t.Context(), org, rolesMember, opensocial.ActionInvite)
	require.NoError(t, err)
	require.True(t, ok)
	set, err := s.AssignableRoles(t.Context(), org, rolesMember)
	require.NoError(t, err)
	require.Equal(t, []string{"viewer"}, set.Roles())

	// The editor cannot hand the admin role anything it does not hold.
	escalating := []habitat.NetworkHabitatOpensocialDefsActionBinding{
		{Action: string(opensocial.ActionInvite), Roles: []string{"member", "editor"}},
		{Action: string(opensocial.ActionEject), Roles: []string{"member"}},
	}
	require.ErrorIs(
		t,
		s.UpdatePermissions(t.Context(), org, rolesEditor, escalating, assignable),
		opensocial.ErrEscalation,
	)
}
