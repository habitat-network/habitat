package relationship

import (
	"slices"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/habitat-network/habitat/internal/events"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const orgDID = syntax.DID("did:plc:org")

func newTestStore(t *testing.T) (*store, spaces.Store, fgastore.Store) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })
	eventStore, err := events.NewStore(db)
	require.NoError(t, err)
	spacesStore, err := spaces.NewStore(db, fga, eventStore)
	require.NoError(t, err)
	return NewStore(spacesStore, fga), spacesStore, fga
}

func newSpace(t *testing.T, spacesStore spaces.Store, skey string) habitat_syntax.SpaceURI {
	t.Helper()
	uri, err := spacesStore.CreateSpace(
		t.Context(),
		orgDID,
		orgDID,
		"network.habitat.space",
		habitat_syntax.SpaceKey(skey),
	)
	require.NoError(t, err)
	return uri
}

func TestWriteTupleAndCheck(t *testing.T) {
	s, spacesStore, _ := newTestStore(t)
	space := newSpace(t, spacesStore, "a")
	alice := syntax.DID("did:plc:alice")

	_, err := s.WriteTuple(
		t.Context(),
		UserSubject{DID: alice},
		RoleWriter,
		SpaceObject{Space: space},
	)
	require.NoError(t, err)

	ok, err := s.Check(t.Context(), alice, RoleWriter, space)
	require.NoError(t, err)
	require.True(t, ok, "alice should be a writer")

	ok, err = s.Check(t.Context(), alice, RoleReader, space)
	require.NoError(t, err)
	require.True(t, ok, "writer implies reader")
}

func TestWriteTupleInvalidCombination(t *testing.T) {
	s, spacesStore, _ := newTestStore(t)
	space := newSpace(t, spacesStore, "a")

	// "member" is not a valid relation on a space object.
	_, err := s.WriteTuple(
		t.Context(),
		UserSubject{DID: "did:plc:alice"},
		RoleMember,
		SpaceObject{Space: space},
	)
	require.ErrorIs(t, err, ErrInvalidTuple)
}

func TestWriteTupleMissingSpace(t *testing.T) {
	s, _, _ := newTestStore(t)
	missing := habitat_syntax.ConstructSpaceURI(orgDID, "network.habitat.space", "nope")
	_, err := s.WriteTuple(
		t.Context(),
		UserSubject{DID: "did:plc:alice"},
		RoleReader,
		SpaceObject{Space: missing},
	)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
}

func TestGroupMembershipResolves(t *testing.T) {
	s, spacesStore, _ := newTestStore(t)
	space := newSpace(t, spacesStore, "a")
	bob := syntax.DID("did:plc:bob")

	group, err := s.CreateGroup(t.Context(), space, "team", "the team")
	require.NoError(t, err)

	// bob ∈ group, and the group reads the space.
	_, err = s.WriteTuple(t.Context(), UserSubject{DID: bob}, RoleMember, GroupObject{Group: group})
	require.NoError(t, err)
	_, err = s.WriteTuple(
		t.Context(),
		GroupSubject{Group: group},
		RoleReader,
		SpaceObject{Space: space},
	)
	require.NoError(t, err)

	ok, err := s.Check(t.Context(), bob, RoleReader, space)
	require.NoError(t, err)
	require.True(t, ok, "group member should inherit the group's space role")

	groups, err := s.ListGroups(t.Context(), space)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, "team", groups[0].Name)
}

func TestCrossSpaceInheritance(t *testing.T) {
	s, spacesStore, _ := newTestStore(t)
	spaceA := newSpace(t, spacesStore, "a")
	spaceB := newSpace(t, spacesStore, "b")
	alice := syntax.DID("did:plc:alice")

	_, err := s.WriteTuple(
		t.Context(),
		UserSubject{DID: alice},
		RoleWriter,
		SpaceObject{Space: spaceA},
	)
	require.NoError(t, err)
	// Writers of A may write B.
	_, err = s.WriteTuple(
		t.Context(),
		SpaceRoleSubject{Space: spaceA, Role: RoleWriter},
		RoleWriter,
		SpaceObject{Space: spaceB},
	)
	require.NoError(t, err)

	ok, err := s.Check(t.Context(), alice, RoleWriter, spaceB)
	require.NoError(t, err)
	require.True(t, ok, "writers of A should inherit write on B")
}

func TestOrgRoleResolves(t *testing.T) {
	s, spacesStore, fga := newTestStore(t)
	space := newSpace(t, spacesStore, "a")
	carol := syntax.DID("did:plc:carol")

	// Mirror an org membership as the org store would.
	require.NoError(t, fga.Write(
		t.Context(),
		fgastore.MemberUserString(carol),
		fgastore.RelationMember,
		fgastore.OrgObjectKey(orgDID),
	))
	// Every org member reads the space.
	_, err := s.WriteTuple(
		t.Context(),
		OrgRoleSubject{Org: orgDID, Role: RoleMember},
		RoleReader,
		SpaceObject{Space: space},
	)
	require.NoError(t, err)

	ok, err := s.Check(t.Context(), carol, RoleReader, space)
	require.NoError(t, err)
	require.True(t, ok, "org member should inherit the org's space role")
}

func TestListTuplesAndFilters(t *testing.T) {
	s, spacesStore, _ := newTestStore(t)
	space := newSpace(t, spacesStore, "a")
	alice := syntax.DID("did:plc:alice")
	bob := syntax.DID("did:plc:bob")

	_, err := s.WriteTuple(
		t.Context(),
		UserSubject{DID: alice},
		RoleWriter,
		SpaceObject{Space: space},
	)
	require.NoError(t, err)
	_, err = s.WriteTuple(t.Context(), UserSubject{DID: bob}, RoleReader, SpaceObject{Space: space})
	require.NoError(t, err)

	all, err := s.ListTuples(t.Context(), space, TupleFilter{})
	require.NoError(t, err)
	require.Len(t, all, 2)

	onlyAlice, err := s.ListTuples(t.Context(), space, TupleFilter{SubjectDID: alice.String()})
	require.NoError(t, err)
	require.Len(t, onlyAlice, 1)
	require.Equal(t, alice, onlyAlice[0].Subject.(UserSubject).DID)

	writers, err := s.ListTuples(t.Context(), space, TupleFilter{Relation: string(RoleWriter)})
	require.NoError(t, err)
	require.Len(t, writers, 1)
}

func TestDeleteTuple(t *testing.T) {
	s, spacesStore, _ := newTestStore(t)
	space := newSpace(t, spacesStore, "a")
	alice := syntax.DID("did:plc:alice")

	uri, err := s.WriteTuple(
		t.Context(),
		UserSubject{DID: alice},
		RoleWriter,
		SpaceObject{Space: space},
	)
	require.NoError(t, err)

	require.NoError(t, s.DeleteTuple(t.Context(), uri))

	ok, err := s.Check(t.Context(), alice, RoleWriter, space)
	require.NoError(t, err)
	require.False(t, ok, "tuple should no longer resolve after delete")

	remaining, err := s.ListTuples(t.Context(), space, TupleFilter{})
	require.NoError(t, err)
	require.Empty(t, remaining)

	require.ErrorIs(t, s.DeleteTuple(t.Context(), uri), ErrTupleNotFound)
}

func TestListSubjectsExpandsUsersets(t *testing.T) {
	s, spacesStore, _ := newTestStore(t)
	space := newSpace(t, spacesStore, "a")
	alice := syntax.DID("did:plc:alice")
	bob := syntax.DID("did:plc:bob")

	group, err := s.CreateGroup(t.Context(), space, "team", "")
	require.NoError(t, err)
	_, err = s.WriteTuple(
		t.Context(),
		UserSubject{DID: alice},
		RoleReader,
		SpaceObject{Space: space},
	)
	require.NoError(t, err)
	_, err = s.WriteTuple(t.Context(), UserSubject{DID: bob}, RoleMember, GroupObject{Group: group})
	require.NoError(t, err)
	_, err = s.WriteTuple(
		t.Context(),
		GroupSubject{Group: group},
		RoleReader,
		SpaceObject{Space: space},
	)
	require.NoError(t, err)

	dids, err := s.ListSubjects(t.Context(), space, RoleReader)
	require.NoError(t, err)
	require.True(t, slices.Contains(dids, alice), "alice reads directly")
	require.True(t, slices.Contains(dids, bob), "bob reads via group")
	require.True(t, slices.Contains(dids, orgDID), "org owner reads via contextual owner tuple")
}

func TestDeleteGroupRemovesMembershipsAndReferences(t *testing.T) {
	s, spacesStore, _ := newTestStore(t)
	space := newSpace(t, spacesStore, "a")
	bob := syntax.DID("did:plc:bob")

	group, err := s.CreateGroup(t.Context(), space, "team", "")
	require.NoError(t, err)
	_, err = s.WriteTuple(t.Context(), UserSubject{DID: bob}, RoleMember, GroupObject{Group: group})
	require.NoError(t, err)
	_, err = s.WriteTuple(
		t.Context(),
		GroupSubject{Group: group},
		RoleReader,
		SpaceObject{Space: space},
	)
	require.NoError(t, err)

	require.NoError(t, s.DeleteGroup(t.Context(), group))

	ok, err := s.Check(t.Context(), bob, RoleReader, space)
	require.NoError(t, err)
	require.False(t, ok, "deleting the group should drop bob's inherited access")

	groups, err := s.ListGroups(t.Context(), space)
	require.NoError(t, err)
	require.Empty(t, groups)

	// All tuple records referencing the group should be gone too.
	tuples, err := s.ListTuples(t.Context(), space, TupleFilter{})
	require.NoError(t, err)
	require.Empty(t, tuples)

	require.ErrorIs(t, s.DeleteGroup(t.Context(), group), ErrGroupNotFound)
}
