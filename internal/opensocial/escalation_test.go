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

func TestUpdateSpaceAccess_Escalation(t *testing.T) {
	s := opensocial_testutil.NewTestStore(t)
	creator := syntax.DID("did:plc:acc-creator")
	member := syntax.DID("did:plc:acc-member")
	orgStr, err := s.NewOrg(t.Context(), "acc-acme", creator)
	require.NoError(t, err)
	org := syntax.DID(orgStr)
	require.NoError(t, s.AssignRoles(t.Context(), org, member, []string{opensocial.MemberRoleRkey}))
	meta := habitat_syntax.ConstructSpaceURI(org, opensocial.MetaSpaceType, "self")

	t.Run("a missing space is reported before the grants are judged", func(t *testing.T) {
		missing := habitat_syntax.ConstructSpaceURI(org, "network.habitat.missing", "self")
		require.Error(t, s.UpdateSpaceAccess(t.Context(), missing, member, nil))
	})

	t.Run("a member who may assign no roles cannot change who reads", func(t *testing.T) {
		err := s.UpdateSpaceAccess(
			t.Context(), meta, member, []string{opensocial.AdminRoleRkey},
		)
		require.ErrorIs(t, err, opensocial.ErrEscalation)
	})

	t.Run("an admin may change who reads", func(t *testing.T) {
		require.NoError(t, s.UpdateSpaceAccess(
			t.Context(), meta, creator, []string{opensocial.AdminRoleRkey},
		))
	})

	t.Run("the group DID may change who reads", func(t *testing.T) {
		require.NoError(t, s.UpdateSpaceAccess(
			t.Context(), meta, org,
			[]string{opensocial.AdminRoleRkey, opensocial.MemberRoleRkey},
		))
	})
}

func TestUpdatePermissions_CannotAssignRolesBeyondOwn(t *testing.T) {
	s := opensocial_testutil.NewTestStore(t)
	creator := syntax.DID("did:plc:asg-creator")
	orgStr, err := s.NewOrg(t.Context(), "asg-acme", creator)
	require.NoError(t, err)
	org := syntax.DID(orgStr)
	delegate := syntax.DID("did:plc:asg-delegate")
	require.NoError(t, s.PutRole(t.Context(), org, "delegate", "Delegate", ""))
	require.NoError(t, s.AssignRoles(t.Context(), org, delegate, []string{"delegate"}))
	configure := string(opensocial.ActionCommunityConfigure)
	bindings := []habitat.NetworkHabitatOpensocialDefsActionBinding{
		{Action: configure, Roles: []string{"delegate", opensocial.AdminRoleRkey}},
		{
			Action: string(opensocial.ActionMcpConfigure),
			Roles:  []string{opensocial.AdminRoleRkey},
		},
	}
	require.NoError(t, s.PutPermissions(
		t.Context(), org, bindings,
		[]habitat.NetworkHabitatOpensocialDefsAssignableBinding{
			{Role: "delegate", Roles: []string{opensocial.MemberRoleRkey}},
			{Role: opensocial.AdminRoleRkey, Roles: []string{opensocial.AdminRoleRkey, "delegate"}},
		},
	))

	err = s.UpdatePermissions(
		t.Context(), org, delegate, bindings,
		[]habitat.NetworkHabitatOpensocialDefsAssignableBinding{
			{
				Role:  "delegate",
				Roles: []string{opensocial.MemberRoleRkey, opensocial.AdminRoleRkey},
			},
			{Role: opensocial.AdminRoleRkey, Roles: []string{opensocial.AdminRoleRkey, "delegate"}},
		},
	)
	require.ErrorIs(t, err, opensocial.ErrEscalation, "the delegate may not make admin assignable")
}
