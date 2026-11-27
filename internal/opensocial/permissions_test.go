package opensocial_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// TestPermissions exercises the roles/capabilities layer: the permissions
// record bootstrapped by NewOrg, CheckAction, AssignableRoles/CanAssignRoles,
// and role CRUD.
func TestPermissions(t *testing.T) {
	s := opensocial_testutil.NewTestStore(t)
	creator := syntax.DID("did:plc:creator")

	orgDIDStr, err := s.NewOrg(t.Context(), "acme", creator)
	require.NoError(t, err)
	org := syntax.DID(orgDIDStr)

	t.Run("BootstrapPermissions", func(t *testing.T) {
		// The creator, as admin, is authorized to perform every standardized
		// action.
		for _, action := range []opensocial.Action{
			opensocial.ActionInvite,
			opensocial.ActionEject,
			opensocial.ActionRoleAssign,
			opensocial.ActionSpaceCreate,
			opensocial.ActionSpaceConfigure,
			opensocial.ActionSpaceDelete,
			opensocial.ActionCommunityConfigure,
			opensocial.ActionMcpConfigure,
		} {
			ok, err := s.CheckAction(t.Context(), org, creator, action)
			require.NoError(t, err)
			require.Truef(t, ok, "admin should be authorized for %s", action)
		}

		// A plain member is authorized for nothing by default.
		member := syntax.DID("did:plc:member")
		require.NoError(
			t, s.AssignRoles(t.Context(), org, member, []string{opensocial.MemberRoleRkey}),
		)
		ok, err := s.CheckAction(t.Context(), org, member, opensocial.ActionCommunityConfigure)
		require.NoError(t, err)
		require.False(t, ok)

		// A stranger holding no roles is authorized for nothing.
		ok, err = s.CheckAction(
			t.Context(), org, syntax.DID("did:plc:stranger"), opensocial.ActionInvite,
		)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("PutPermissionsBindsNewRoleToAction", func(t *testing.T) {
		require.NoError(t, s.PutRole(t.Context(), org, "moderator", "Moderator", ""))
		moderator := syntax.DID("did:plc:moderator")
		require.NoError(
			t, s.AssignRoles(t.Context(), org, moderator, []string{"moderator"}),
		)

		require.NoError(t, s.PutPermissions(
			t.Context(), org,
			[]habitat.NetworkHabitatOpensocialDefsActionBinding{
				{Action: string(opensocial.ActionEject), Roles: []string{opensocial.AdminRoleRkey}},
				{
					Action: string(opensocial.ActionSpaceConfigure),
					Roles:  []string{"moderator", opensocial.AdminRoleRkey},
				},
			},
			[]habitat.NetworkHabitatOpensocialDefsAssignableBinding{
				{
					Role:  opensocial.AdminRoleRkey,
					Roles: []string{opensocial.MemberRoleRkey, "moderator"},
				},
			},
		))

		ok, err := s.CheckAction(t.Context(), org, moderator, opensocial.ActionSpaceConfigure)
		require.NoError(t, err)
		require.True(t, ok)

		// The rewrite dropped every other binding, including the admin's
		// community.configure binding seeded by NewOrg.
		ok, err = s.CheckAction(t.Context(), org, creator, opensocial.ActionCommunityConfigure)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("AssignableRolesAndCanAssignRoles", func(t *testing.T) {
		require.NoError(t, s.PutPermissions(
			t.Context(), org,
			[]habitat.NetworkHabitatOpensocialDefsActionBinding{
				{
					Action: string(opensocial.ActionRoleAssign),
					Roles:  []string{opensocial.AdminRoleRkey},
				},
			},
			[]habitat.NetworkHabitatOpensocialDefsAssignableBinding{
				{Role: opensocial.AdminRoleRkey, Roles: []string{opensocial.MemberRoleRkey}},
			},
		))

		assignable, err := s.AssignableRoles(t.Context(), org, creator)
		require.NoError(t, err)
		require.Equal(t, []string{opensocial.MemberRoleRkey}, assignable)

		// The admin may move a member between no roles and the member role...
		ok, err := s.CanAssignRoles(
			t.Context(), org, creator, nil, []string{opensocial.MemberRoleRkey},
		)
		require.NoError(t, err)
		require.True(t, ok)

		// ...but not grant the admin role, which isn't in its assignable set.
		ok, err = s.CanAssignRoles(
			t.Context(), org, creator, nil, []string{opensocial.AdminRoleRkey},
		)
		require.NoError(t, err)
		require.False(t, ok)

		// A member with no assignable roles can assign nothing.
		member := syntax.DID("did:plc:plain-member")
		require.NoError(
			t, s.AssignRoles(t.Context(), org, member, []string{opensocial.MemberRoleRkey}),
		)
		ok, err = s.CanAssignRoles(
			t.Context(), org, member, nil, []string{opensocial.MemberRoleRkey},
		)
		require.NoError(t, err)
		require.False(t, ok)

		// No-op reassignment (unchanged role set) is always allowed.
		ok, err = s.CanAssignRoles(
			t.Context(), org, member,
			[]string{opensocial.MemberRoleRkey}, []string{opensocial.MemberRoleRkey},
		)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("PutAndDeleteRole", func(t *testing.T) {
		require.NoError(t, s.PutRole(t.Context(), org, "vip", "VIP", "Special member"))

		record, err := s.SpaceStore.GetRecord(
			t.Context(),
			habitat_syntax.ConstructSpaceURI(org, opensocial.MembersSpaceType, "self"),
			org, "group.opensocial.role", "vip",
		)
		require.NoError(t, err)
		require.Equal(t, "VIP", record.Value["displayName"])

		require.NoError(t, s.DeleteRole(t.Context(), org, "vip"))
		_, err = s.SpaceStore.GetRecord(
			t.Context(),
			habitat_syntax.ConstructSpaceURI(org, opensocial.MembersSpaceType, "self"),
			org, "group.opensocial.role", "vip",
		)
		require.Error(t, err)

		// Deleting a role that doesn't exist fails.
		err = s.DeleteRole(t.Context(), org, "nonexistent")
		require.ErrorIs(t, err, opensocial.ErrRoleNotFound)

		// The built-in roles cannot be deleted.
		err = s.DeleteRole(t.Context(), org, opensocial.AdminRoleRkey)
		require.ErrorIs(t, err, opensocial.ErrCannotDeleteBuiltinRole)
		err = s.DeleteRole(t.Context(), org, opensocial.MemberRoleRkey)
		require.ErrorIs(t, err, opensocial.ErrCannotDeleteBuiltinRole)
	})

	t.Run("EjectMember", func(t *testing.T) {
		toEject := syntax.DID("did:plc:to-eject")
		require.NoError(
			t, s.AssignRoles(t.Context(), org, toEject, []string{opensocial.MemberRoleRkey}),
		)

		require.NoError(t, s.EjectMember(t.Context(), org, toEject))

		roles, err := s.GetUserRoles(t.Context(), org, toEject)
		require.NoError(t, err)
		require.Empty(t, roles)

		// Ejecting a non-member fails.
		err = s.EjectMember(t.Context(), org, syntax.DID("did:plc:never-was-a-member"))
		require.ErrorIs(t, err, opensocial.ErrMemberNotFound)
	})
}

// TestPermissionsLegacyOrgFallback covers a community that predates this
// authz layer and so never had NewOrg write it a permissions record: its
// admins must still be authorized for every action and able to assign/eject
// any declared role, or nobody could ever hold the community.configure
// action needed to write that first permissions record.
func TestPermissionsLegacyOrgFallback(t *testing.T) {
	s := opensocial_testutil.NewTestStore(t)
	creator := syntax.DID("did:plc:legacy-creator")

	orgDIDStr, err := s.NewOrg(t.Context(), "legacy-acme", creator)
	require.NoError(t, err)
	org := syntax.DID(orgDIDStr)

	// Simulate a pre-existing community by deleting the permissions record
	// NewOrg bootstrapped.
	require.NoError(t, s.SpaceStore.DeleteRecord(
		t.Context(),
		habitat_syntax.ConstructSpaceURI(org, opensocial.MembersSpaceType, "self"),
		org,
		opensocial.PermissionsCollection,
		"self",
	))

	ok, err := s.CheckAction(t.Context(), org, creator, opensocial.ActionCommunityConfigure)
	require.NoError(t, err)
	require.True(t, ok, "admin should fall back to authorized for every action")

	member := syntax.DID("did:plc:legacy-member")
	require.NoError(
		t, s.AssignRoles(t.Context(), org, member, []string{opensocial.MemberRoleRkey}),
	)
	ok, err = s.CheckAction(t.Context(), org, member, opensocial.ActionCommunityConfigure)
	require.NoError(t, err)
	require.False(t, ok, "a non-admin gets no fallback")

	assignable, err := s.AssignableRoles(t.Context(), org, creator)
	require.NoError(t, err)
	require.ElementsMatch(
		t,
		[]string{opensocial.AdminRoleRkey, opensocial.MemberRoleRkey},
		assignable,
	)

	assignable, err = s.AssignableRoles(t.Context(), org, member)
	require.NoError(t, err)
	require.Empty(t, assignable, "a non-admin gets no fallback")

	// The admin can now write a real permissions record, ending the
	// fallback.
	require.NoError(t, s.PutPermissions(
		t.Context(), org,
		[]habitat.NetworkHabitatOpensocialDefsActionBinding{
			{
				Action: string(opensocial.ActionCommunityConfigure),
				Roles:  []string{opensocial.AdminRoleRkey},
			},
		},
		nil,
	))
	assignable, err = s.AssignableRoles(t.Context(), org, creator)
	require.NoError(t, err)
	require.Empty(t, assignable, "the fallback no longer applies once permissions are configured")
}

func TestPermissionsGroupDIDAndEscalation(t *testing.T) {
	s := opensocial_testutil.NewTestStore(t)
	creator := syntax.DID("did:plc:esc-creator")
	orgDIDStr, err := s.NewOrg(t.Context(), "esc-acme", creator)
	require.NoError(t, err)
	org := syntax.DID(orgDIDStr)

	t.Run("the group DID is implicitly authorized and may assign any role", func(t *testing.T) {
		ok, err := s.CheckAction(t.Context(), org, org, opensocial.ActionMcpConfigure)
		require.NoError(t, err)
		require.True(t, ok)
		assignable, err := s.AssignableRoles(t.Context(), org, org)
		require.NoError(t, err)
		require.ElementsMatch(
			t, []string{opensocial.AdminRoleRkey, opensocial.MemberRoleRkey}, assignable,
		)
	})

	// delegate may reconfigure the group and assign only the member role, and
	// holds only the scopes of those two actions.
	require.NoError(t, s.PutRole(t.Context(), org, "delegate", "Delegate", ""))
	delegate := syntax.DID("did:plc:delegate")
	require.NoError(t, s.AssignRoles(t.Context(), org, delegate, []string{"delegate"}))
	require.NoError(t, s.PutPermissions(
		t.Context(), org,
		[]habitat.NetworkHabitatOpensocialDefsActionBinding{
			{Action: string(opensocial.ActionCommunityConfigure), Roles: []string{"delegate"}},
			{Action: string(opensocial.ActionCommunityConfigure), Roles: []string{opensocial.AdminRoleRkey}},
			{Action: string(opensocial.ActionMcpConfigure), Roles: []string{opensocial.AdminRoleRkey}},
		},
		[]habitat.NetworkHabitatOpensocialDefsAssignableBinding{
			{Role: "delegate", Roles: []string{opensocial.MemberRoleRkey}},
			{Role: opensocial.AdminRoleRkey, Roles: []string{opensocial.AdminRoleRkey, "delegate"}},
		},
	))

	t.Run("a delegate cannot grant a scope it does not hold", func(t *testing.T) {
		err := s.UpdatePermissions(
			t.Context(), org, delegate,
			[]habitat.NetworkHabitatOpensocialDefsActionBinding{
				{Action: string(opensocial.ActionMcpConfigure), Roles: []string{opensocial.MemberRoleRkey}},
			},
			[]habitat.NetworkHabitatOpensocialDefsAssignableBinding{
				{Role: "delegate", Roles: []string{opensocial.MemberRoleRkey}},
			},
		)
		require.ErrorIs(t, err, opensocial.ErrEscalation)
	})

	t.Run("a delegate cannot change a role it may not assign", func(t *testing.T) {
		err := s.UpdatePermissions(
			t.Context(), org, delegate,
			[]habitat.NetworkHabitatOpensocialDefsActionBinding{
				{Action: string(opensocial.ActionCommunityConfigure), Roles: []string{"delegate"}},
			},
			[]habitat.NetworkHabitatOpensocialDefsAssignableBinding{
				{Role: "delegate", Roles: []string{opensocial.MemberRoleRkey}},
			},
		)
		require.ErrorIs(t, err, opensocial.ErrEscalation, "dropping the admin role's grants changes admin")
	})

	t.Run("an admin may rewrite permissions it already holds", func(t *testing.T) {
		current, err := s.GetPermissions(t.Context(), org)
		require.NoError(t, err)
		bindings, assignable := opensocial.ActionBindings(current.Grants)
		require.NoError(t, s.UpdatePermissions(t.Context(), org, creator, bindings, assignable))
	})
}

func TestCheckPermissionPublicReadAndGroupDID(t *testing.T) {
	s := opensocial_testutil.NewTestStore(t)
	creator := syntax.DID("did:plc:pub-creator")
	orgDIDStr, err := s.NewOrg(t.Context(), "pub-acme", creator)
	require.NoError(t, err)
	org := syntax.DID(orgDIDStr)
	stranger := syntax.DID("did:plc:pub-stranger")
	meta := habitat_syntax.ConstructSpaceURI(org, opensocial.MetaSpaceType, "self")
	members := habitat_syntax.ConstructSpaceURI(org, opensocial.MembersSpaceType, "self")

	ok, err := s.CheckPermission(t.Context(), stranger, meta)
	require.NoError(t, err)
	require.True(t, ok, "the meta space's access record is publicRead")

	ok, err = s.CheckPermission(t.Context(), stranger, members)
	require.NoError(t, err)
	require.False(t, ok, "the members space is not public")

	ok, err = s.CheckPermission(t.Context(), org, members)
	require.NoError(t, err)
	require.True(t, ok, "the group DID can read its own spaces")
}

func TestUpdateSpaceKeepsOtherScopesAndPublicRead(t *testing.T) {
	s := opensocial_testutil.NewTestStore(t)
	creator := syntax.DID("did:plc:keep-creator")
	orgDIDStr, err := s.NewOrg(t.Context(), "keep-acme", creator)
	require.NoError(t, err)
	org := syntax.DID(orgDIDStr)
	meta := habitat_syntax.ConstructSpaceURI(org, opensocial.MetaSpaceType, "self")

	// UpdateSpace changes who can read, not what else the access record says.
	require.NoError(t, s.UpdateSpace(t.Context(), meta, []string{opensocial.AdminRoleRkey}))
	ok, err := s.CheckPermission(t.Context(), syntax.DID("did:plc:keep-stranger"), meta)
	require.NoError(t, err)
	require.True(t, ok, "publicRead survives an UpdateSpace")
}
