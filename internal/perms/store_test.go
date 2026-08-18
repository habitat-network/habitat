package perms

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/fgastore"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	org   = syntax.DID("did:plc:org")
	alice = syntax.DID("did:plc:alice")
	bob   = syntax.DID("did:plc:bob")

	docsType  = syntax.NSID("network.habitat.docs")
	groupType = syntax.NSID("network.habitat.group")
)

func newTestStore(t *testing.T) *store {
	t.Helper()
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })
	return NewStore(fga)
}

// newSpace returns a space URI owned by org, without creating any backing
// records — perms only cares about the URI as an FGA object key.
func newSpace(spaceType syntax.NSID, skey string) habitat_syntax.SpaceURI {
	return habitat_syntax.ConstructSpaceURI(org, spaceType, habitat_syntax.SpaceKey(skey))
}

func TestStoreAddUserRelationAndCheckUserHasSpaceRole(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	space := newSpace(docsType, "doc1")

	t.Run("no relation is denied", func(t *testing.T) {
		ok, err := s.CheckUserHasSpaceRole(ctx, alice, space, SpaceRoleReader)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("granted role is allowed", func(t *testing.T) {
		require.NoError(t, s.AddUserRelation(ctx, alice, space, SpaceRoleReader))
		ok, err := s.CheckUserHasSpaceRole(ctx, alice, space, SpaceRoleReader)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("granting is idempotent", func(t *testing.T) {
		require.NoError(t, s.AddUserRelation(ctx, alice, space, SpaceRoleReader))
	})

	t.Run("a role does not imply a higher role", func(t *testing.T) {
		ok, err := s.CheckUserHasSpaceRole(ctx, alice, space, SpaceRoleWriter)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("a higher role implies a lower role", func(t *testing.T) {
		require.NoError(t, s.AddUserRelation(ctx, bob, space, SpaceRoleWriter))
		ok, err := s.CheckUserHasSpaceRole(ctx, bob, space, SpaceRoleReader)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("unrelated user is denied", func(t *testing.T) {
		ok, err := s.CheckUserHasSpaceRole(ctx, "did:plc:stranger", space, SpaceRoleReader)
		require.NoError(t, err)
		require.False(t, ok)
	})
}

func TestStoreCheckUserHasSpaceRoleImplicitAccess(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	space := newSpace(docsType, "doc1")

	t.Run("space owner is an implicit owner without a stored tuple", func(t *testing.T) {
		ok, err := s.CheckUserHasSpaceRole(ctx, space.SpaceOwner(), space, SpaceRoleOwner)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("space owner is an implicit reader too", func(t *testing.T) {
		ok, err := s.CheckUserHasSpaceRole(ctx, space.SpaceOwner(), space, SpaceRoleReader)
		require.NoError(t, err)
		require.True(t, ok)
	})
}

func TestStoreRevokeUserRelation(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	space := newSpace(docsType, "doc1")

	require.NoError(t, s.AddUserRelation(ctx, alice, space, SpaceRoleReader))
	ok, err := s.CheckUserHasSpaceRole(ctx, alice, space, SpaceRoleReader)
	require.NoError(t, err)
	require.True(t, ok)

	require.NoError(t, s.RevokeUserRelation(ctx, alice, space, SpaceRoleReader))
	ok, err = s.CheckUserHasSpaceRole(ctx, alice, space, SpaceRoleReader)
	require.NoError(t, err)
	require.False(t, ok)

	t.Run("revoking a relation that doesn't exist is a no-op", func(t *testing.T) {
		require.NoError(t, s.RevokeUserRelation(ctx, alice, space, SpaceRoleReader))
	})
}

func TestStoreSpaceRoleRelation(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	group := newSpace(groupType, "team")
	doc := newSpace(docsType, "doc1")

	t.Run("subject without the subject role is denied", func(t *testing.T) {
		require.NoError(
			t,
			s.AddSpaceRoleRelation(ctx, group, SpaceRoleReader, doc, SpaceRoleReader),
		)
		ok, err := s.CheckUserHasSpaceRole(ctx, alice, doc, SpaceRoleReader)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("granting the space-role relation propagates to its subjects", func(t *testing.T) {
		require.NoError(t, s.AddUserRelation(ctx, alice, group, SpaceRoleReader))
		ok, err := s.CheckUserHasSpaceRole(ctx, alice, doc, SpaceRoleReader)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run(
		"CheckSpaceRelationHasSpaceRole reports the userset relation directly",
		func(t *testing.T) {
			ok, err := s.CheckSpaceRelationHasSpaceRole(
				ctx,
				group,
				SpaceRoleReader,
				doc,
				SpaceRoleReader,
			)
			require.NoError(t, err)
			require.True(t, ok)

			ok, err = s.CheckSpaceRelationHasSpaceRole(
				ctx,
				group,
				SpaceRoleReader,
				doc,
				SpaceRoleWriter,
			)
			require.NoError(t, err)
			require.False(t, ok)
		},
	)

	t.Run("revoking the space-role relation removes access", func(t *testing.T) {
		require.NoError(
			t,
			s.RevokeSpaceRoleRelation(ctx, group, SpaceRoleReader, doc, SpaceRoleReader),
		)
		ok, err := s.CheckUserHasSpaceRole(ctx, alice, doc, SpaceRoleReader)
		require.NoError(t, err)
		require.False(t, ok)

		t.Run("revoking again is a no-op", func(t *testing.T) {
			require.NoError(
				t,
				s.RevokeSpaceRoleRelation(ctx, group, SpaceRoleReader, doc, SpaceRoleReader),
			)
		})
	})
}

func TestStoreUnsafeRevokeAllSpaceRoles(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	space := newSpace(docsType, "doc1")
	group := newSpace(groupType, "team")

	require.NoError(t, s.AddUserRelation(ctx, alice, space, SpaceRoleReader))
	require.NoError(t, s.AddUserRelation(ctx, bob, space, SpaceRoleWriter))
	require.NoError(t, s.AddSpaceRoleRelation(ctx, group, SpaceRoleReader, space, SpaceRoleReader))

	require.NoError(t, s.UnsafeRevokeAllSpaceRoles(ctx, space))

	for _, did := range []syntax.DID{alice, bob} {
		ok, err := s.CheckUserHasSpaceRole(ctx, did, space, SpaceRoleReader)
		require.NoError(t, err)
		require.False(t, ok)
	}

	dids, subjects, err := s.ListSubjects(ctx, space)
	require.NoError(t, err)
	require.Empty(t, dids)
	require.Empty(t, subjects)

	t.Run("no-op on a space with nothing stored", func(t *testing.T) {
		require.NoError(t, s.UnsafeRevokeAllSpaceRoles(ctx, newSpace(docsType, "doc2")))
	})
}

func TestStoreListSubjects(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	space := newSpace(docsType, "doc1")
	group := newSpace(groupType, "team")

	require.NoError(t, s.AddUserRelation(ctx, alice, space, SpaceRoleReader))
	require.NoError(t, s.AddUserRelation(ctx, bob, space, SpaceRoleWriter))
	require.NoError(t, s.AddSpaceRoleRelation(ctx, group, SpaceRoleReader, space, SpaceRoleReader))

	dids, subjects, err := s.ListSubjects(ctx, space)
	require.NoError(t, err)
	require.ElementsMatch(t, []syntax.DID{alice, bob}, dids)
	require.ElementsMatch(t, []SpaceRoleSubject{{Space: group, Role: SpaceRoleReader}}, subjects)

	t.Run("ListUserSubjects returns only the DIDs", func(t *testing.T) {
		got, err := s.ListUserSubjects(ctx, space)
		require.NoError(t, err)
		require.ElementsMatch(t, []syntax.DID{alice, bob}, got)
	})

	t.Run("ListSpaceRoleSubjects returns only the space-userset grantees", func(t *testing.T) {
		got, err := s.ListSpaceRoleSubjects(ctx, space)
		require.NoError(t, err)
		require.ElementsMatch(t, []SpaceRoleSubject{{Space: group, Role: SpaceRoleReader}}, got)
	})

	t.Run("empty when nothing is stored", func(t *testing.T) {
		dids, subjects, err := s.ListSubjects(ctx, newSpace(docsType, "doc2"))
		require.NoError(t, err)
		require.Empty(t, dids)
		require.Empty(t, subjects)
	})
}

func TestStoreListObjects(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	doc1 := newSpace(docsType, "doc1")
	doc2 := newSpace(docsType, "doc2")
	doc3 := newSpace(docsType, "doc3")

	require.NoError(t, s.AddUserRelation(ctx, alice, doc1, SpaceRoleReader))
	require.NoError(t, s.AddUserRelation(ctx, alice, doc2, SpaceRoleWriter))
	require.NoError(t, s.AddUserRelation(ctx, bob, doc3, SpaceRoleReader))

	got, err := s.ListObjects(ctx, alice)
	require.NoError(t, err)
	require.ElementsMatch(t, []habitat_syntax.SpaceURI{doc1, doc2}, got)

	t.Run("empty for a did with no relations", func(t *testing.T) {
		got, err := s.ListObjects(ctx, "did:plc:stranger")
		require.NoError(t, err)
		require.Empty(t, got)
	})
}
