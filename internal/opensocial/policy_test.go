package opensocial_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func groupPolicy(grants ...spaces.GroupGrant) spaces.Policy {
	return spaces.Policy{Type: spaces.PolicyGroupSpace, Grants: grants}
}

// indexedSpaces returns the space refs in org's space index.
func indexedSpaces(
	t *testing.T,
	s *opensocial_testutil.TestStore,
	org syntax.DID,
) []string {
	t.Helper()
	collection := syntax.NSID(opensocial.SpaceIndexCollection)
	records, err := s.SpaceStore.ListRecords(
		t.Context(), habitat_syntax.ConstructSpaceURI(org, opensocial.MembersSpaceType, "self"),
		org, &collection,
	)
	require.NoError(t, err)
	var out []string
	for _, record := range records {
		ref, ok := record.Value["space"].(string)
		require.True(t, ok)
		out = append(out, ref)
	}
	return out
}

func TestCreateAndDeleteGroupSpace(t *testing.T) {
	s, org := newRolesOrg(t)
	policy := groupPolicy(spaces.GroupGrant{Role: "member", Read: true, Write: true})

	uri, err := s.CreateGroupSpace(t.Context(), org, rolesAdmin, "network.habitat.docs", "main", policy)
	require.NoError(t, err)
	require.Contains(t, indexedSpaces(t, s, org), uri.AtSpace().String())
	stored, err := s.GetPolicy(t.Context(), uri)
	require.NoError(t, err)
	require.Equal(t, []string{"member"}, stored.WriteRoles())

	t.Run("a policy that leaves the space unlisted is not indexed", func(t *testing.T) {
		unlisted := policy
		unlisted.Unlisted = true
		hidden, err := s.CreateGroupSpace(
			t.Context(), org, rolesAdmin, "network.habitat.docs", "hidden", unlisted,
		)
		require.NoError(t, err)
		require.NotContains(t, indexedSpaces(t, s, org), hidden.AtSpace().String())
	})

	t.Run("callers need manage=create and reserved or malformed policies are refused", func(t *testing.T) {
		_, err := s.CreateGroupSpace(
			t.Context(), org, rolesMember, "network.habitat.docs", "no", policy,
		)
		require.ErrorIs(t, err, opensocial.ErrEscalation)
		_, err = s.CreateGroupSpace(
			t.Context(), org, rolesAdmin, opensocial.MembersSpaceType, "x", policy,
		)
		require.ErrorIs(t, err, opensocial.ErrReservedSpace)
		_, err = s.CreateGroupSpace(
			t.Context(), org, rolesAdmin, "network.habitat.docs", "ghost",
			groupPolicy(spaces.GroupGrant{Role: "ghost", Read: true}),
		)
		require.ErrorIs(t, err, opensocial.ErrUnknownRole)
		_, err = s.CreateGroupSpace(
			t.Context(), org, rolesAdmin, "network.habitat.docs", "bad",
			groupPolicy(spaces.GroupGrant{Role: "member", Permissions: []string{"nonsense"}}),
		)
		require.ErrorIs(t, err, opensocial.ErrInvalidScope)
	})

	t.Run("an editor cannot grant permissions they lack", func(t *testing.T) {
		require.NoError(t, s.PutRole(t.Context(), org, "maker", opensocial.Role{
			Permissions: []string{"space:*?manage=create"}, CanAssign: []string{"viewer"},
		}))
		require.NoError(t, s.AssignRoles(t.Context(), org, "did:plc:maker", []string{"maker"}))
		_, err := s.CreateGroupSpace(
			t.Context(), org, "did:plc:maker", "network.habitat.docs", "escalate",
			groupPolicy(spaces.GroupGrant{
				Role: "viewer", Permissions: []string{"include:org.atgrp.authManageAdmin"},
			}),
		)
		require.ErrorIs(t, err, opensocial.ErrEscalation)
		_, err = s.CreateGroupSpace(
			t.Context(), org, "did:plc:maker", "network.habitat.docs", "fine",
			groupPolicy(spaces.GroupGrant{Role: "viewer", Read: true}),
		)
		require.NoError(t, err)
	})

	t.Run("deleting removes the space and its index entry", func(t *testing.T) {
		require.ErrorIs(
			t, s.DeleteGroupSpace(t.Context(), org, rolesMember, uri), opensocial.ErrEscalation,
		)
		require.NoError(t, s.DeleteGroupSpace(t.Context(), org, rolesAdmin, uri))
		require.NotContains(t, indexedSpaces(t, s, org), uri.AtSpace().String())
		require.ErrorIs(
			t, s.DeleteGroupSpace(t.Context(), org, rolesAdmin, uri), spaces.ErrSpaceNotFound,
		)
	})

	t.Run("another group's space and reserved spaces cannot be deleted", func(t *testing.T) {
		otherStr, err := s.NewOrg(t.Context(), "policy-other", rolesAdmin)
		require.NoError(t, err)
		otherSpace, err := s.CreateGroupSpace(
			t.Context(), syntax.DID(otherStr), rolesAdmin, "network.habitat.docs", "self", policy,
		)
		require.NoError(t, err)
		require.ErrorIs(
			t, s.DeleteGroupSpace(t.Context(), org, rolesAdmin, otherSpace), spaces.ErrSpaceNotFound,
		)
		members := habitat_syntax.ConstructSpaceURI(org, opensocial.MembersSpaceType, "self")
		require.ErrorIs(
			t, s.DeleteGroupSpace(t.Context(), org, rolesAdmin, members), opensocial.ErrReservedSpace,
		)
	})
}

func TestUpdatePolicy(t *testing.T) {
	s, org := newRolesOrg(t)
	uri, err := s.CreateGroupSpace(
		t.Context(), org, rolesAdmin, "network.habitat.docs", "main",
		groupPolicy(spaces.GroupGrant{Role: "editor", Read: true}),
	)
	require.NoError(t, err)

	t.Run("unlisting removes the index entry and listing restores it", func(t *testing.T) {
		current, err := s.GetPolicy(t.Context(), uri)
		require.NoError(t, err)
		current.Unlisted = true
		require.NoError(t, s.UpdatePolicy(t.Context(), uri, rolesAdmin, current))
		require.NotContains(t, indexedSpaces(t, s, org), uri.AtSpace().String())
		current.Unlisted = false
		require.NoError(t, s.UpdatePolicy(t.Context(), uri, rolesAdmin, current))
		require.Contains(t, indexedSpaces(t, s, org), uri.AtSpace().String())
	})

	t.Run("a role may only change grants for roles it can assign", func(t *testing.T) {
		// The editor reads the space and may assign viewer.
		require.NoError(t, s.UpdatePolicy(t.Context(), uri, rolesEditor, groupPolicy(
			spaces.GroupGrant{Role: "editor", Read: true},
			spaces.GroupGrant{Role: "viewer", Read: true},
		)))
		for _, updated := range []spaces.Policy{
			// admin is not assignable by the editor
			groupPolicy(
				spaces.GroupGrant{Role: "editor", Read: true},
				spaces.GroupGrant{Role: "admin", Read: true},
			),
			// the editor cannot write the space, so cannot grant write
			groupPolicy(
				spaces.GroupGrant{Role: "editor", Read: true},
				spaces.GroupGrant{Role: "viewer", Read: true, Write: true},
			),
			// permissions the editor lacks
			groupPolicy(
				spaces.GroupGrant{Role: "editor", Read: true},
				spaces.GroupGrant{
					Role: "viewer", Read: true,
					Permissions: []string{"include:org.atgrp.authManageAdmin"},
				},
			),
		} {
			require.ErrorIs(
				t, s.UpdatePolicy(t.Context(), uri, rolesEditor, updated), opensocial.ErrEscalation,
			)
		}
	})

	t.Run("public read needs read access", func(t *testing.T) {
		public := groupPolicy(spaces.GroupGrant{Role: "editor", Read: true})
		public.PublicRead = true
		require.ErrorIs(
			t, s.UpdatePolicy(t.Context(), uri, rolesMember, public), opensocial.ErrEscalation,
		)
		require.NoError(t, s.UpdatePolicy(t.Context(), uri, rolesEditor, public))
	})

	t.Run("an unknown space is reported", func(t *testing.T) {
		missing := habitat_syntax.ConstructSpaceURI(org, "network.habitat.docs", "missing")
		require.ErrorIs(
			t, s.UpdatePolicy(t.Context(), missing, rolesAdmin, groupPolicy()), spaces.ErrSpaceNotFound,
		)
	})
}

func TestSpaceAccessChecks(t *testing.T) {
	s, org := newRolesOrg(t)
	uri, err := s.CreateGroupSpace(
		t.Context(), org, rolesAdmin, "network.habitat.docs", "main",
		groupPolicy(
			spaces.GroupGrant{Role: "member", Read: true},
			spaces.GroupGrant{Role: "editor", Read: true, Write: true, Permissions: []string{
				"space:network.habitat.docs?collection=app.comment&action=create",
			}},
		),
	)
	require.NoError(t, err)
	stranger := syntax.DID("did:plc:stranger")

	for _, tc := range []struct {
		name        string
		user        syntax.DID
		read, write bool
	}{
		{"the group", org, true, true},
		{"a reader", rolesMember, true, false},
		{"a writer", rolesEditor, true, true},
		{"a stranger", stranger, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read, err := s.CheckPermission(t.Context(), tc.user, uri)
			require.NoError(t, err)
			require.Equal(t, tc.read, read)
			write, err := s.CheckWrite(t.Context(), tc.user, uri)
			require.NoError(t, err)
			require.Equal(t, tc.write, write)
		})
	}

	t.Run("public read does not admit writers", func(t *testing.T) {
		public, err := s.GetPolicy(t.Context(), uri)
		require.NoError(t, err)
		public.PublicRead = true
		require.NoError(t, s.UpdatePolicy(t.Context(), uri, org, public))
		read, err := s.CheckPermission(t.Context(), stranger, uri)
		require.NoError(t, err)
		require.True(t, read)
		write, err := s.CheckWrite(t.Context(), stranger, uri)
		require.NoError(t, err)
		require.False(t, write)
	})

	t.Run("the members space is open to every member", func(t *testing.T) {
		members := habitat_syntax.ConstructSpaceURI(org, opensocial.MembersSpaceType, "self")
		require.NoError(t, s.AssignRoles(t.Context(), org, "did:plc:custom", []string{"viewer"}))
		read, err := s.CheckPermission(t.Context(), "did:plc:custom", members)
		require.NoError(t, err)
		require.True(t, read)
		write, err := s.CheckWrite(t.Context(), "did:plc:custom", members)
		require.NoError(t, err)
		require.True(t, write)
		read, err = s.CheckPermission(t.Context(), stranger, members)
		require.NoError(t, err)
		require.False(t, read)
	})

	t.Run("space scopes add the policy's permissions to the role's own", func(t *testing.T) {
		scopes, err := s.SpaceScopes(t.Context(), rolesEditor, uri)
		require.NoError(t, err)
		require.True(t, scopes.Covers("space:network.habitat.docs?collection=app.comment&action=create"))
		plain, err := s.UserScopes(t.Context(), org, rolesEditor)
		require.NoError(t, err)
		require.False(t, plain.Covers("space:network.habitat.docs?collection=app.comment&action=create"))
		groupScopes, err := s.SpaceScopes(t.Context(), org, uri)
		require.NoError(t, err)
		require.True(t, groupScopes.Covers("rpc:anything"))
	})

	t.Run("missing and non-group spaces grant nothing", func(t *testing.T) {
		missing := habitat_syntax.ConstructSpaceURI(org, "network.habitat.docs", "missing")
		read, err := s.CheckPermission(t.Context(), rolesMember, missing)
		require.NoError(t, err)
		require.False(t, read)
		write, err := s.CheckWrite(t.Context(), rolesMember, missing)
		require.NoError(t, err)
		require.False(t, write)

		plain, err := s.SpaceStore.CreateSpace(t.Context(), org, "network.habitat.docs", "plain")
		require.NoError(t, err)
		read, err = s.CheckPermission(t.Context(), rolesMember, plain)
		require.NoError(t, err)
		require.False(t, read)
		write, err = s.CheckWrite(t.Context(), rolesMember, plain)
		require.NoError(t, err)
		require.False(t, write)
	})
}

func TestJoinAndLeave(t *testing.T) {
	s := newTestStore(t)
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

	t.Run("a new group is invite-only and admits the member role by default", func(t *testing.T) {
		policy, err := s.JoinPolicy(t.Context(), org)
		require.NoError(t, err)
		require.Equal(t, opensocial.JoinPolicyInvite, policy)
		defaults, err := s.DefaultRoles(t.Context(), org)
		require.NoError(t, err)
		require.Equal(t, []string{opensocial.MemberRoleRkey}, defaults)
	})

	t.Run("the config record sets the join policy", func(t *testing.T) {
		require.NoError(t, s.UpdateProfile(t.Context(), org, "JL", "", opensocial.JoinPolicyOpen))
		policy, err := s.JoinPolicy(t.Context(), org)
		require.NoError(t, err)
		require.Equal(t, opensocial.JoinPolicyOpen, policy)
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
