package opensocial_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestStoreAuthorizeGroupWrite(t *testing.T) {
	ts := opensocial_testutil.NewTestStore(t)
	ctx := t.Context()
	admin := syntax.DID("did:plc:admin")
	steward := syntax.DID("did:plc:steward")
	member := syntax.DID("did:plc:member")
	orgID, err := ts.NewOrg(ctx, "acme", admin)
	require.NoError(t, err)
	group := syntax.DID(orgID)

	// A steward may assign members and hold the member role, nothing more.
	require.NoError(t, ts.PutRole(ctx, group, "steward", opensocial.Role{
		DisplayName: "Steward",
		Permissions: []string{"include:org.atgrp.authManageMembers"},
		CanAssign:   []string{opensocial.MemberRoleRkey},
	}))
	require.NoError(t, ts.AssignRoles(ctx, group, steward, []string{
		opensocial.MemberRoleRkey, "steward",
	}))
	require.NoError(t, ts.AssignRoles(ctx, group, member, []string{opensocial.MemberRoleRkey}))

	members := habitat_syntax.ConstructSpaceURI(group, opensocial.MembersSpaceType, "self")
	write := func(actor syntax.DID, collection string, rkey string, value map[string]any) error {
		_, scopes, err := ts.SessionScopes(ctx, group, group, actor, nil)
		require.NoError(t, err)
		return ts.AuthorizeGroupWrite(ctx, opensocial.GroupWrite{
			Group: group, Actor: actor, Scopes: scopes, Space: members,
			Collection: syntax.NSID(collection), Rkey: syntax.RecordKey(rkey), Value: value,
		})
	}
	membership := func(roles ...string) map[string]any {
		return map[string]any{"roles": roles, "createdAt": "2026-01-01T00:00:00Z"}
	}

	t.Run("host-maintained records are refused to everyone", func(t *testing.T) {
		require.ErrorIs(t, write(admin, opensocial.SpaceIndexCollection, "x", map[string]any{}),
			opensocial.ErrHostMaintained)
		require.ErrorIs(t, write(group, opensocial.SpacePolicyCollection, "self", map[string]any{}),
			opensocial.ErrHostMaintained)
	})

	t.Run("an actor needs the space scope for the collection", func(t *testing.T) {
		require.ErrorIs(t, write(member, opensocial.RuleCollection, "r1", map[string]any{}),
			opensocial.ErrEscalation)
		require.NoError(t, write(admin, opensocial.RuleCollection, "r1", map[string]any{}))
	})

	t.Run("membership changes stay within the roles the actor may assign", func(t *testing.T) {
		require.NoError(t, write(steward, opensocial.MembershipCollection, string(member),
			membership(opensocial.MemberRoleRkey)))
		require.ErrorIs(t, write(
			steward,
			opensocial.MembershipCollection,
			string(member),
			membership(
				opensocial.MemberRoleRkey,
				opensocial.AdminRoleRkey,
			),
		), opensocial.ErrEscalation)
		require.NoError(t, write(admin, opensocial.MembershipCollection, string(member),
			membership(opensocial.MemberRoleRkey, opensocial.AdminRoleRkey)))
	})

	t.Run("role records cannot add permissions the actor lacks", func(t *testing.T) {
		require.ErrorIs(t, write(
			steward,
			opensocial.RoleCollection,
			"steward",
			map[string]any{
				"permissions": []string{
					"include:org.atgrp.authManageMembers",
					"include:org.atgrp.authModerate",
				},
			},
		),
			opensocial.ErrEscalation, "steward may not assign its own role")
		require.NoError(t, write(admin, opensocial.RoleCollection, "poster",
			map[string]any{"permissions": []string{"include:org.atgrp.authParticipate"}}))
		require.ErrorIs(t, write(admin, opensocial.RoleCollection, opensocial.AdminRoleRkey, nil),
			opensocial.ErrCannotDeleteBuiltinRole)
	})

	t.Run("the group never loses its last admin", func(t *testing.T) {
		// With a second admin, the first may step down once.
		require.ErrorIs(t, write(admin, opensocial.MembershipCollection, string(admin),
			membership(opensocial.MemberRoleRkey)), opensocial.ErrLastAdmin, "the only admin")
		require.NoError(t, ts.AssignRoles(
			ctx, group, member, []string{opensocial.MemberRoleRkey, opensocial.AdminRoleRkey},
		))
		require.NoError(t, write(admin, opensocial.MembershipCollection, string(admin),
			membership(opensocial.MemberRoleRkey)))
		require.NoError(t, ts.AssignRoles(ctx, group, admin, []string{opensocial.MemberRoleRkey}))
		require.ErrorIs(t, write(group, opensocial.MembershipCollection, string(member),
			membership(opensocial.MemberRoleRkey)), opensocial.ErrLastAdmin)
		require.ErrorIs(t, write(group, opensocial.MembershipCollection, string(member), nil),
			opensocial.ErrLastAdmin)
		require.ErrorIs(t, ts.EjectMember(ctx, group, member), opensocial.ErrLastAdmin)
		require.ErrorIs(t, ts.LeaveGroup(ctx, group, member), opensocial.ErrLastAdmin)
	})

	t.Run("other spaces are not guarded", func(t *testing.T) {
		docs, err := ts.CreateSpace(
			ctx, group, []string{opensocial.MemberRoleRkey}, "network.habitat.docs", "d",
		)
		require.NoError(t, err)
		require.NoError(t, ts.AuthorizeGroupWrite(ctx, opensocial.GroupWrite{
			Group: group, Actor: member, Space: docs, Collection: "network.habitat.docs.note",
			Rkey: "n", Value: map[string]any{},
		}))
	})
}
