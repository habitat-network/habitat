package opensocial_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func readAccess(role string) opensocial_api.GroupOpensocialDefsAccessConfig {
	return opensocial_api.GroupOpensocialDefsAccessConfig{
		Grants: []opensocial_api.GroupOpensocialDefsSpaceGrant{
			{Role: role, Scopes: []string{"read"}},
		},
	}
}

func TestGroupSpaces_CreateAndDelete(t *testing.T) {
	s := opensocial_testutil.NewTestStore(t)
	admin := syntax.DID("did:plc:gs-admin")
	member := syntax.DID("did:plc:gs-member")
	orgStr, err := s.NewOrg(t.Context(), "gs-acme", admin)
	require.NoError(t, err)
	org := syntax.DID(orgStr)
	require.NoError(t, s.AssignRoles(t.Context(), org, member, []string{opensocial.MemberRoleRkey}))
	policy := spaces.Policy{Type: spaces.PolicyRelationSpace}
	create := func(actor syntax.DID, spaceType syntax.NSID, access opensocial_api.GroupOpensocialDefsAccessConfig) error {
		_, err := s.CreateGroupSpace(
			t.Context(),
			org,
			actor,
			spaceType,
			"self",
			policy,
			access,
			true,
		)
		return err
	}

	t.Run("reserved space types are refused", func(t *testing.T) {
		require.ErrorIs(
			t,
			create(admin, opensocial.MetaSpaceType, readAccess("member")),
			opensocial.ErrReservedSpace,
		)
	})

	t.Run("a member without manage=create is refused", func(t *testing.T) {
		require.ErrorIs(
			t,
			create(member, "network.habitat.docs", readAccess("member")),
			opensocial.ErrEscalation,
		)
	})

	t.Run("grants must name declared roles and valid scopes the caller holds", func(t *testing.T) {
		require.ErrorIs(
			t,
			create(admin, "network.habitat.docs", readAccess("ghost")),
			opensocial.ErrUnknownRole,
		)
		bad := opensocial_api.GroupOpensocialDefsAccessConfig{
			Grants: []opensocial_api.GroupOpensocialDefsSpaceGrant{
				{Role: "member", Scopes: []string{"nonsense"}},
			},
		}
		require.ErrorIs(t, create(admin, "network.habitat.docs", bad), opensocial.ErrInvalidScope)
	})

	t.Run("an admin creates an indexed space and then deletes it", func(t *testing.T) {
		created, err := s.CreateGroupSpace(
			t.Context(), org, admin, "network.habitat.docs", "self", policy,
			readAccess("member"), true,
		)
		require.NoError(t, err)
		require.NotNil(t, created.Index)

		require.ErrorIs(
			t,
			s.DeleteGroupSpace(t.Context(), org, member, created.Space),
			opensocial.ErrEscalation,
		)
		require.NoError(t, s.DeleteGroupSpace(t.Context(), org, admin, created.Space))
		require.ErrorIs(
			t, s.DeleteGroupSpace(t.Context(), org, admin, created.Space), spaces.ErrSpaceNotFound,
		)
	})

	t.Run("another group's space and reserved spaces cannot be deleted", func(t *testing.T) {
		other, err := s.NewOrg(t.Context(), "gs-other", admin)
		require.NoError(t, err)
		otherSpace, err := s.CreateGroupSpace(
			t.Context(), syntax.DID(other), admin, "network.habitat.docs", "self", policy,
			readAccess("member"), false,
		)
		require.NoError(t, err)
		require.ErrorIs(
			t, s.DeleteGroupSpace(t.Context(), org, admin, otherSpace.Space),
			spaces.ErrSpaceNotFound,
		)
		members := habitat_syntax.ConstructSpaceURI(org, opensocial.MembersSpaceType, "self")
		require.ErrorIs(
			t, s.DeleteGroupSpace(t.Context(), org, admin, members), opensocial.ErrReservedSpace,
		)
	})
}

func TestGroupSpaces_JoinAndLeave(t *testing.T) {
	s := opensocial_testutil.NewTestStore(t)
	admin := syntax.DID("did:plc:jl-admin")
	joiner := syntax.DID("did:plc:jl-joiner")
	orgStr, err := s.NewOrg(t.Context(), "jl-acme", admin)
	require.NoError(t, err)
	org := syntax.DID(orgStr)

	t.Run("an unknown group has no join policy and cannot be left", func(t *testing.T) {
		ghost := syntax.DID("did:plc:jl-ghost")
		_, err := s.JoinPolicy(t.Context(), ghost)
		require.ErrorIs(t, err, opensocial.ErrGroupNotFound)
		require.ErrorIs(t, s.LeaveGroup(t.Context(), ghost, joiner), opensocial.ErrGroupNotFound)
	})

	t.Run("a new group admits invitees only, with the member role by default", func(t *testing.T) {
		policy, err := s.JoinPolicy(t.Context(), org)
		require.NoError(t, err)
		require.Equal(t, "invite", policy)
		defaults, err := s.DefaultRoles(t.Context(), org)
		require.NoError(t, err)
		require.Equal(t, []string{opensocial.MemberRoleRkey}, defaults)
	})

	t.Run("admit grants the default roles once and leaving is idempotent", func(t *testing.T) {
		ref, roles, err := s.Admit(t.Context(), org, joiner)
		require.NoError(t, err)
		require.NotEmpty(t, ref.URI)
		require.Equal(t, []string{opensocial.MemberRoleRkey}, roles)

		_, again, err := s.Admit(t.Context(), org, joiner)
		require.NoError(t, err)
		require.Equal(t, roles, again, "an existing member keeps their roles")

		require.NoError(t, s.LeaveGroup(t.Context(), org, joiner))
		require.NoError(t, s.LeaveGroup(t.Context(), org, joiner))
		held, err := s.GetUserRoles(t.Context(), org, joiner)
		require.NoError(t, err)
		require.Empty(t, held)
	})
}
