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
	orgB  = syntax.DID("did:plc:orgB")
	alice = syntax.DID("did:plc:alice")
	bob   = syntax.DID("did:plc:bob")

	docsType  = syntax.NSID("network.habitat.docs")
	groupType = syntax.NSID("network.habitat.group")
)

// newTestStore returns a perms Store, along with the spaces.Store backing
// it — AddUserRelation/AddSpaceRoleRelation persist a relationship record
// into the space itself, so tests exercising those need the space to
// actually exist (see newSpace).
func newTestStore(t *testing.T) *store {
	t.Helper()
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })

	db := db_testutil.NewDB(t)
	sp := spaces_testutil.NewTestStore(t, spaces_testutil.Config{DB: db, FgaStore: fga})

	return NewStore(db, sp.Store, fga)
}

// newSpace creates a space owned by org in sp and returns its URI.
// AddUserRelation/AddSpaceRoleRelation write their relationship record via
// the spaces store, which requires the space to already exist.
func newSpace(
	t *testing.T,
	sp spaces.Store,
	spaceType syntax.NSID,
	skey string,
) habitat_syntax.SpaceURI {
	t.Helper()
	uri, err := sp.CreateSpace(t.Context(), org, org, spaceType, habitat_syntax.SpaceKey(skey))
	require.NoError(t, err)
	return uri
}

func TestStoreAddUserRelationAndCheckUserHasSpaceRole(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	space := newSpace(t, s.spaces, docsType, "doc1")

	t.Run("no relation is denied", func(t *testing.T) {
		ok, err := s.CheckUserHasSpaceRole(ctx, alice, space, habitat_syntax.SpaceRoleReader, org)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("granted role is allowed", func(t *testing.T) {
		uri, err := s.SetUserRelation(ctx, alice, space, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
		require.Equal(t, habitat_syntax.UserRelationCollection, uri.Collection())
		require.Equal(t, space, uri.SpaceURI())

		ok, err := s.CheckUserHasSpaceRole(ctx, alice, space, habitat_syntax.SpaceRoleReader, org)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("granting is idempotent", func(t *testing.T) {
		_, err := s.SetUserRelation(ctx, alice, space, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
	})

	t.Run("a role does not imply a higher role", func(t *testing.T) {
		ok, err := s.CheckUserHasSpaceRole(ctx, alice, space, habitat_syntax.SpaceRoleWriter, org)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("a higher role implies a lower role", func(t *testing.T) {
		_, err := s.SetUserRelation(ctx, bob, space, habitat_syntax.SpaceRoleWriter)
		require.NoError(t, err)
		ok, err := s.CheckUserHasSpaceRole(ctx, bob, space, habitat_syntax.SpaceRoleReader, org)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("unrelated user is denied", func(t *testing.T) {
		ok, err := s.CheckUserHasSpaceRole(
			ctx,
			"did:plc:stranger",
			space,
			habitat_syntax.SpaceRoleReader,
			org,
		)
		require.NoError(t, err)
		require.False(t, ok)
	})
}

func TestStoreCheckUserHasSpaceRoleImplicitAccess(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	space := newSpace(t, s.spaces, docsType, "doc1")

	t.Run("space owner is an implicit owner without a stored tuple", func(t *testing.T) {
		ok, err := s.CheckUserHasSpaceRole(
			ctx,
			space.SpaceOwner(),
			space,
			habitat_syntax.SpaceRoleOwner,
			org,
		)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("space owner is an implicit reader too", func(t *testing.T) {
		ok, err := s.CheckUserHasSpaceRole(
			ctx,
			space.SpaceOwner(),
			space,
			habitat_syntax.SpaceRoleReader,
			org,
		)
		require.NoError(t, err)
		require.True(t, ok)
	})
}

// TestStoreCheckUserHasSpaceRoleUsesCallerOrgForImplicitAccess covers the
// case where the caller's org (belongsToOrg) differs from the space's own
// owning org. The implicit-org-member contextual tuple must be built from
// belongsToOrg, the org the caller actually belongs to, not from
// space.SpaceOwner() — otherwise a grant made to another org's members (via
// their org's self-space userset) can never resolve, since the contextual
// tuple would always describe membership in the space's owning org instead.
func TestStoreCheckUserHasSpaceRoleUsesCallerOrgForImplicitAccess(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	// Owned by org, not orgB.
	space := newSpace(t, s.spaces, docsType, "doc1")

	orgBSelfSpace := habitat_syntax.ConstructSpaceURI(
		orgB,
		"network.habitat.organization",
		habitat_syntax.SpaceKey("self"),
	)

	// Grant readers of orgB's self-space (i.e. orgB's members) reader access
	// to space, even though space is owned by a different org.
	_, err := s.SetSpaceRoleRelation(
		ctx,
		orgBSelfSpace,
		habitat_syntax.SpaceRoleReader,
		space,
		habitat_syntax.SpaceRoleReader,
	)
	require.NoError(t, err)

	// alice is a real member of orgB, not of space's owning org.
	require.NoError(t, s.fga.Write(
		ctx,
		fgastore.MemberUserString(alice),
		fgastore.RelationMember,
		fgastore.OrgObjectKey(orgB),
	))

	ok, err := s.CheckUserHasSpaceRole(ctx, alice, space, habitat_syntax.SpaceRoleReader, orgB)
	require.NoError(t, err)
	require.True(
		t,
		ok,
		"alice should have implicit read access via her own org (orgB)'s grant",
	)
}

// TestStoreCheckUserHasSpaceRoleDoesNotLeakAccessFromUnrelatedOrgMembership is
// the negative counterpart: alice really is a member of space's owning org
// (unrelated to this request), but she is not a member of belongsToOrg, the
// org she's actually acting as. Before the fix, CheckUserHasSpaceRole ignored
// belongsToOrg and always built the implicit-org contextual tuple from
// space.SpaceOwner(), so her unrelated membership in the owning org would
// accidentally grant her access. That must not happen.
func TestStoreCheckUserHasSpaceRoleDoesNotLeakAccessFromUnrelatedOrgMembership(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	// Owned by org.
	space := newSpace(t, s.spaces, docsType, "doc1")

	orgSelfSpace := habitat_syntax.ConstructSpaceURI(
		org,
		"network.habitat.organization",
		habitat_syntax.SpaceKey("self"),
	)
	// org's own members are granted reader access to space.
	_, err := s.SetSpaceRoleRelation(
		ctx,
		orgSelfSpace,
		habitat_syntax.SpaceRoleReader,
		space,
		habitat_syntax.SpaceRoleReader,
	)
	require.NoError(t, err)

	// alice really is a member of org, the space's owning org...
	require.NoError(t, s.fga.Write(
		ctx,
		fgastore.MemberUserString(alice),
		fgastore.RelationMember,
		fgastore.OrgObjectKey(org),
	))

	// ...but she is NOT a member of orgB, the org she's actually acting as in
	// this request (belongsToOrg). Her unrelated membership in org must not
	// leak her access to a space just because org happens to own it.
	ok, err := s.CheckUserHasSpaceRole(ctx, alice, space, habitat_syntax.SpaceRoleReader, orgB)
	require.NoError(t, err)
	require.False(t, ok, "alice must not gain access via an org she isn't acting as")
}

func TestStoreRevokeUserRelation(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	space := newSpace(t, s.spaces, docsType, "doc1")

	_, err := s.SetUserRelation(ctx, alice, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)
	ok, err := s.CheckUserHasSpaceRole(ctx, alice, space, habitat_syntax.SpaceRoleReader, org)
	require.NoError(t, err)
	require.True(t, ok)

	require.NoError(t, s.RevokeUserRelation(ctx, alice, space, habitat_syntax.SpaceRoleReader))
	ok, err = s.CheckUserHasSpaceRole(ctx, alice, space, habitat_syntax.SpaceRoleReader, org)
	require.NoError(t, err)
	require.False(t, ok)

	t.Run("revoking a relation that doesn't exist is a no-op", func(t *testing.T) {
		require.NoError(t, s.RevokeUserRelation(ctx, alice, space, habitat_syntax.SpaceRoleReader))
	})
}

func TestStoreSpaceRoleRelation(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	group := newSpace(t, s.spaces, groupType, "team")
	doc := newSpace(t, s.spaces, docsType, "doc1")

	t.Run("subject without the subject role is denied", func(t *testing.T) {
		uri, err := s.SetSpaceRoleRelation(
			ctx,
			group,
			habitat_syntax.SpaceRoleReader,
			doc,
			habitat_syntax.SpaceRoleReader,
		)
		require.NoError(t, err)
		require.Equal(t, habitat_syntax.SpaceRelationCollection, uri.Collection())
		require.Equal(t, doc, uri.SpaceURI())

		ok, err := s.CheckUserHasSpaceRole(ctx, alice, doc, habitat_syntax.SpaceRoleReader, org)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("granting the space-role relation propagates to its subjects", func(t *testing.T) {
		_, err := s.SetUserRelation(ctx, alice, group, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
		ok, err := s.CheckUserHasSpaceRole(ctx, alice, doc, habitat_syntax.SpaceRoleReader, org)
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
			s.RevokeSpaceRoleRelation(
				ctx,
				group,
				habitat_syntax.SpaceRoleReader,
				doc,
				habitat_syntax.SpaceRoleReader,
			),
		)
		ok, err := s.CheckUserHasSpaceRole(ctx, alice, doc, habitat_syntax.SpaceRoleReader, org)
		require.NoError(t, err)
		require.False(t, ok)

		t.Run("revoking again is a no-op", func(t *testing.T) {
			require.NoError(
				t,
				s.RevokeSpaceRoleRelation(
					ctx,
					group,
					habitat_syntax.SpaceRoleReader,
					doc,
					habitat_syntax.SpaceRoleReader,
				),
			)
		})
	})
}

func TestStoreUnsafeRevokeAllSpaceRoles(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	space := newSpace(t, s.spaces, docsType, "doc1")
	group := newSpace(t, s.spaces, groupType, "team")

	_, err := s.SetUserRelation(ctx, alice, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)
	_, err = s.SetUserRelation(ctx, bob, space, habitat_syntax.SpaceRoleWriter)
	require.NoError(t, err)
	_, err = s.SetSpaceRoleRelation(
		ctx,
		group,
		habitat_syntax.SpaceRoleReader,
		space,
		habitat_syntax.SpaceRoleReader,
	)
	require.NoError(t, err)

	require.NoError(t, s.UnsafeRevokeAllSpaceRoles(ctx, space))

	for _, did := range []syntax.DID{alice, bob} {
		ok, err := s.CheckUserHasSpaceRole(ctx, did, space, habitat_syntax.SpaceRoleReader, org)
		require.NoError(t, err)
		require.False(t, ok)
	}

	dids, err := s.ListUserSubjects(ctx, space)
	require.NoError(t, err)
	require.ElementsMatch(t, dids, []syntax.DID{org})

	t.Run("no-op on a space with nothing stored", func(t *testing.T) {
		require.NoError(
			t,
			s.UnsafeRevokeAllSpaceRoles(ctx, newSpace(t, s.spaces, docsType, "doc2")),
		)
	})
}

func TestStoreListUserSubjects(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	space := newSpace(t, s.spaces, docsType, "doc1")
	group := newSpace(t, s.spaces, groupType, "team")

	_, err := s.SetUserRelation(ctx, alice, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)
	_, err = s.SetUserRelation(ctx, bob, space, habitat_syntax.SpaceRoleWriter)
	require.NoError(t, err)
	_, err = s.SetSpaceRoleRelation(
		ctx,
		group,
		habitat_syntax.SpaceRoleReader,
		space,
		habitat_syntax.SpaceRoleReader,
	)
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
	doc1 := newSpace(t, s.spaces, docsType, "doc1")
	doc2 := newSpace(t, s.spaces, docsType, "doc2")
	doc3 := newSpace(t, s.spaces, docsType, "doc3")

	_, err := s.SetUserRelation(ctx, alice, doc1, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)
	_, err = s.SetUserRelation(ctx, alice, doc2, habitat_syntax.SpaceRoleWriter)
	require.NoError(t, err)
	_, err = s.SetUserRelation(ctx, bob, doc3, habitat_syntax.SpaceRoleReader)
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
