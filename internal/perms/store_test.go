package perms

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	org   = syntax.DID("did:plc:org")
	alice = syntax.DID("did:plc:alice")
	bob   = syntax.DID("did:plc:bob")

	docsType  = syntax.NSID("network.habitat.docs")
	groupType = syntax.NSID("network.habitat.group")
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })

	db := db_testutil.NewDB(t)
	sp := spaces_testutil.NewTestStore(t, spaces_testutil.Config{DB: db, FgaStore: fga})

	return NewStore(db, sp.Store, fga)
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
		ok, err := s.CheckUserHasSpaceRole(ctx, alice, space, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("granted role is allowed", func(t *testing.T) {
		uri, err := s.AddUserRelation(ctx, alice, space, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
		require.Equal(t, habitat_syntax.UserRelationCollection, uri.Collection().String())
		require.Equal(t, space, uri.SpaceURI())

		ok, err := s.CheckUserHasSpaceRole(ctx, alice, space, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("granting is idempotent", func(t *testing.T) {
		_, err := s.AddUserRelation(ctx, alice, space, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
	})

	t.Run("a role does not imply a higher role", func(t *testing.T) {
		ok, err := s.CheckUserHasSpaceRole(ctx, alice, space, habitat_syntax.SpaceRoleWriter)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("a higher role implies a lower role", func(t *testing.T) {
		_, err := s.AddUserRelation(ctx, bob, space, habitat_syntax.SpaceRoleWriter)
		require.NoError(t, err)
		ok, err := s.CheckUserHasSpaceRole(ctx, bob, space, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("unrelated user is denied", func(t *testing.T) {
		ok, err := s.CheckUserHasSpaceRole(ctx, "did:plc:stranger", space, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
		require.False(t, ok)
	})
}

func TestStoreCheckUserHasSpaceRoleImplicitAccess(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	space := newSpace(docsType, "doc1")

	t.Run("space owner is an implicit owner without a stored tuple", func(t *testing.T) {
		ok, err := s.CheckUserHasSpaceRole(ctx, space.SpaceOwner(), space, habitat_syntax.SpaceRoleOwner)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("space owner is an implicit reader too", func(t *testing.T) {
		ok, err := s.CheckUserHasSpaceRole(ctx, space.SpaceOwner(), space, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
		require.True(t, ok)
	})
}

func TestStoreRevokeUserRelation(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	space := newSpace(docsType, "doc1")

	_, err := s.AddUserRelation(ctx, alice, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)
	ok, err := s.CheckUserHasSpaceRole(ctx, alice, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)
	require.True(t, ok)

	require.NoError(t, s.RevokeUserRelation(ctx, alice, space, habitat_syntax.SpaceRoleReader))
	ok, err = s.CheckUserHasSpaceRole(ctx, alice, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)
	require.False(t, ok)

	t.Run("revoking a relation that doesn't exist is a no-op", func(t *testing.T) {
		require.NoError(t, s.RevokeUserRelation(ctx, alice, space, habitat_syntax.SpaceRoleReader))
	})
}

func TestStoreSpaceRoleRelation(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	group := newSpace(groupType, "team")
	doc := newSpace(docsType, "doc1")

	t.Run("subject without the subject role is denied", func(t *testing.T) {
		uri, err := s.AddSpaceRoleRelation(ctx, group, habitat_syntax.SpaceRoleReader, doc, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
		require.Equal(t, habitat_syntax.SpaceRelationCollection, uri.Collection().String())
		require.Equal(t, doc, uri.SpaceURI())

		ok, err := s.CheckUserHasSpaceRole(ctx, alice, doc, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("granting the space-role relation propagates to its subjects", func(t *testing.T) {
		_, err := s.AddUserRelation(ctx, alice, group, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
		ok, err := s.CheckUserHasSpaceRole(ctx, alice, doc, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run(
		"CheckSpaceRelationHasSpaceRole reports the userset relation directly",
		func(t *testing.T) {
			ok, err := s.CheckSpaceRelationHasSpaceRole(
				ctx,
				group,
				habitat_syntax.SpaceRoleReader,
				doc,
				habitat_syntax.SpaceRoleReader,
			)
			require.NoError(t, err)
			require.True(t, ok)

			ok, err = s.CheckSpaceRelationHasSpaceRole(
				ctx,
				group,
				habitat_syntax.SpaceRoleReader,
				doc,
				habitat_syntax.SpaceRoleWriter,
			)
			require.NoError(t, err)
			require.False(t, ok)
		},
	)

	t.Run("revoking the space-role relation removes access", func(t *testing.T) {
		require.NoError(
			t,
			s.RevokeSpaceRoleRelation(ctx, group, habitat_syntax.SpaceRoleReader, doc, habitat_syntax.SpaceRoleReader),
		)
		ok, err := s.CheckUserHasSpaceRole(ctx, alice, doc, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
		require.False(t, ok)

		t.Run("revoking again is a no-op", func(t *testing.T) {
			require.NoError(
				t,
				s.RevokeSpaceRoleRelation(ctx, group, habitat_syntax.SpaceRoleReader, doc, habitat_syntax.SpaceRoleReader),
			)
		})
	})
}

func TestStoreUnsafeRevokeAllSpaceRoles(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	space := newSpace(docsType, "doc1")
	group := newSpace(groupType, "team")

	_, err := s.AddUserRelation(ctx, alice, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)
	_, err = s.AddUserRelation(ctx, bob, space, habitat_syntax.SpaceRoleWriter)
	require.NoError(t, err)
	_, err = s.AddSpaceRoleRelation(ctx, group, habitat_syntax.SpaceRoleReader, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)

	require.NoError(t, s.UnsafeRevokeAllSpaceRoles(ctx, space))

	for _, did := range []syntax.DID{alice, bob} {
		ok, err := s.CheckUserHasSpaceRole(ctx, did, space, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
		require.False(t, ok)
	}

	dids, err := s.ListUserSubjects(ctx, space)
	require.NoError(t, err)
	require.ElementsMatch(t, dids, []syntax.DID{org})

	t.Run("no-op on a space with nothing stored", func(t *testing.T) {
		require.NoError(t, s.UnsafeRevokeAllSpaceRoles(ctx, newSpace(docsType, "doc2")))
	})
}

func TestStoreListUserSubjects(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	space := newSpace(docsType, "doc1")
	group := newSpace(groupType, "team")

	_, err := s.AddUserRelation(ctx, alice, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)
	_, err = s.AddUserRelation(ctx, bob, space, habitat_syntax.SpaceRoleWriter)
	require.NoError(t, err)
	_, err = s.AddSpaceRoleRelation(ctx, group, habitat_syntax.SpaceRoleReader, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)

	t.Run("ListUserSubjects returns only the DIDs", func(t *testing.T) {
		got, err := s.ListUserSubjects(ctx, space)
		require.NoError(t, err)
		require.ElementsMatch(t, []syntax.DID{alice, bob, org}, got)
	})
}

func TestStoreListObjects(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	doc1 := newSpace(docsType, "doc1")
	doc2 := newSpace(docsType, "doc2")
	doc3 := newSpace(docsType, "doc3")

	_, err := s.AddUserRelation(ctx, alice, doc1, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)
	_, err = s.AddUserRelation(ctx, alice, doc2, habitat_syntax.SpaceRoleWriter)
	require.NoError(t, err)
	_, err = s.AddUserRelation(ctx, bob, doc3, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)

	got, err := s.ListObjects(ctx, alice)
	require.NoError(t, err)
	require.ElementsMatch(t, []habitat_syntax.SpaceURI{doc1, doc2}, got)

	t.Run("empty for a did with no relations", func(t *testing.T) {
		got, err := s.ListObjects(ctx, "did:plc:stranger")
		require.NoError(t, err)
		require.Empty(t, got)
	})
}
