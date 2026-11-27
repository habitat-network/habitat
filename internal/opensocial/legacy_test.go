package opensocial_test

import (
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestMigrateLegacyGroups(t *testing.T) {
	ctx := t.Context()
	s := opensocial_testutil.NewTestStore(t)
	org := syntax.DID("did:plc:acme")
	alice := syntax.DID("did:plc:alice")
	bob := syntax.DID("did:plc:bob")

	members := habitat_syntax.ConstructSpaceURI(org, "community.opensocial.members", "self")
	about := habitat_syntax.ConstructSpaceURI(org, "community.opensocial.about", "self")
	docs := habitat_syntax.ConstructSpaceURI(org, "network.habitat.docs", "shared")
	secret := habitat_syntax.ConstructSpaceURI(org, "network.habitat.docs", "secret")
	for _, space := range []habitat_syntax.SpaceURI{members, about, docs, secret} {
		_, err := s.SpaceStore.CreateSpace(
			ctx, space.SpaceOwner(), syntax.NSID(space.SpaceType()), space.Skey(),
		)
		require.NoError(t, err)
	}
	put := func(
		space habitat_syntax.SpaceURI, repo syntax.DID, collection string, rkey string,
		value map[string]any,
	) {
		value["$type"] = collection
		raw, err := spaces.MarshalRecord(value)
		require.NoError(t, err)
		_, _, err = s.SpaceStore.PutRecord(
			ctx, space, repo, syntax.NSID(collection), syntax.RecordKey(rkey), raw,
		)
		require.NoError(t, err)
	}
	put(about, org, "community.opensocial.profile", "self",
		map[string]any{"name": "Acme", "description": "We make anvils"})
	put(members, org, "community.opensocial.role", "admin", map[string]any{"name": "Admin"})
	put(members, org, "community.opensocial.role", "member", map[string]any{"name": "Member"})
	put(members, org, "community.opensocial.role", "editor", map[string]any{"name": "Editor"})
	put(members, org, "community.opensocial.permissions", "self", map[string]any{
		"bindings": []any{
			map[string]any{"action": "invite", "roles": []any{"admin", "editor"}},
			map[string]any{"action": "eject", "roles": []any{"admin"}},
		},
		"assignable": []any{
			map[string]any{"role": "admin", "roles": []any{"admin", "member", "editor"}},
			map[string]any{"role": "editor", "roles": []any{"member"}},
		},
	})
	put(members, org, "community.opensocial.membership", string(alice),
		map[string]any{"roles": []any{"member", "admin"}, "updatedAt": "2026-01-01T00:00:00Z"})
	put(members, org, "community.opensocial.membership", string(bob),
		map[string]any{"roles": []any{"editor"}})
	put(members, alice, "community.opensocial.acceptance", "self",
		map[string]any{"updatedAt": "2026-01-02T00:00:00Z"})
	put(members, alice, "community.opensocial.memberProfile", "self",
		map[string]any{"displayName": "Alice"})
	put(docs, org, "community.opensocial.access", "self",
		map[string]any{"roles": []any{"member", "ghost"}})
	put(secret, org, "community.opensocial.access", "self",
		map[string]any{"roles": []any{"admin"}})
	put(members, org, "community.opensocial.space", "3kabc",
		map[string]any{"uri": docs.AtSpace().String()})
	require.NoError(t, s.DB.Exec(
		"INSERT INTO invite_rows (id, org_id, invitee, roles, created_at) VALUES (?, ?, ?, ?, ?)",
		"inv1", org, "did:plc:carol", `["editor"]`, time.Now(),
	).Error)

	require.NoError(t, s.MigrateLegacyGroups(ctx))
	// Running it again finds nothing left to migrate.
	require.NoError(t, s.MigrateLegacyGroups(ctx))

	newMembers := habitat_syntax.ConstructSpaceURI(org, opensocial.MembersSpaceType, "self")
	newMeta := habitat_syntax.ConstructSpaceURI(org, opensocial.MetaSpaceType, "self")

	for _, old := range []habitat_syntax.SpaceURI{members, about} {
		exists, err := s.SpaceStore.CheckSpaceExists(ctx, old)
		require.NoError(t, err)
		require.False(t, exists, old)
	}

	t.Run("profile becomes about and config", func(t *testing.T) {
		record, err := s.SpaceStore.GetRecord(ctx, newMeta, org, opensocial.AboutCollection, "self")
		require.NoError(t, err)
		require.Equal(t, "Acme", record.Value["displayName"])
		require.Equal(t, "We make anvils", record.Value["description"])
		policy, err := s.JoinPolicy(ctx, org)
		require.NoError(t, err)
		require.Equal(t, opensocial.JoinPolicyInvite, policy)
	})

	t.Run("roles carry the scopes of their bound actions", func(t *testing.T) {
		roles, err := s.GetRoles(ctx, org)
		require.NoError(t, err)
		require.Len(t, roles, 3)
		require.Equal(
			t,
			opensocial.ActionScopes(opensocial.ActionInvite),
			roles["editor"].Permissions,
		)
		require.Equal(t, []string{"member"}, roles["editor"].CanAssign)
		require.True(t, roles["member"].Default)

		scopes, err := s.UserScopes(ctx, org, alice)
		require.NoError(t, err)
		require.True(t, scopes.CanPerform(opensocial.ActionEject))
		bobScopes, err := s.UserScopes(ctx, org, bob)
		require.NoError(t, err)
		require.True(t, bobScopes.CanPerform(opensocial.ActionInvite))
		require.False(t, bobScopes.CanPerform(opensocial.ActionEject))
	})

	t.Run("memberships, confirmations and profiles move across", func(t *testing.T) {
		held, err := s.GetUserRoles(ctx, org, alice)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"member", "admin"}, held)
		confirmation, err := s.SpaceStore.GetRecord(
			ctx, newMembers, alice, opensocial.ConfirmationCollection, "self",
		)
		require.NoError(t, err)
		require.Equal(t, "2026-01-02T00:00:00Z", confirmation.Value["createdAt"])
		_, err = s.SpaceStore.GetRecord(
			ctx, newMembers, alice, opensocial.MemberProfileCollection, "self",
		)
		require.NoError(t, err)
	})

	t.Run("access records become policies and the index follows", func(t *testing.T) {
		policy, err := s.GetPolicy(ctx, docs)
		require.NoError(t, err)
		require.Equal(t, []string{"member"}, policy.ReadRoles())
		require.Equal(t, []string{"member"}, policy.WriteRoles())
		require.False(t, policy.Unlisted)
		_, err = s.SpaceStore.GetRecord(ctx, docs, org, "community.opensocial.access", "self")
		require.ErrorIs(t, err, spaces.ErrRecordNotFound)

		// The old index did not list secret.
		hidden, err := s.GetPolicy(ctx, secret)
		require.NoError(t, err)
		require.True(t, hidden.Unlisted)
		indexed, err := s.SpaceStore.ListCollection(
			ctx,
			newMembers,
			opensocial.SpaceIndexCollection,
		)
		require.NoError(t, err)
		require.Len(t, indexed, 1)
	})

	t.Run("invite rows become inbox records", func(t *testing.T) {
		carol := syntax.DID("did:plc:carol")
		invites, err := s.SpaceStore.ListCollection(
			ctx,
			habitat_syntax.ConstructSpaceURI(carol, opensocial.InboxSpaceType, "self"),
			opensocial.InviteCollection,
		)
		require.NoError(t, err)
		require.Len(t, invites, 1)
		require.Equal(t, org, invites[0].Owner)
		var rows int64
		require.NoError(t, s.DB.Table("invite_rows").Count(&rows).Error)
		require.Zero(t, rows)
	})
}
