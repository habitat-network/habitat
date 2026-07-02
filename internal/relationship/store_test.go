package relationship

import (
	"context"
	"errors"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/events"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	org   = syntax.DID("did:plc:org")
	alice = syntax.DID("did:plc:alice")
	bob   = syntax.DID("did:plc:bob")

	docsType  = syntax.NSID("network.habitat.docs")
	groupType = syntax.NSID("network.habitat.group")
)

func newTestStore(t *testing.T) (*Store, spaces.Store) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	require.NoError(t, err)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })
	eventStore, err := events.NewStore(db)
	require.NoError(t, err)
	sp, err := spaces.NewStore(db, fga, eventStore)
	require.NoError(t, err)
	return NewStore(db, sp, fga), sp
}

// newSpace creates a space owned by org and returns its URI.
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

func TestParseCheckSubject(t *testing.T) {
	space := habitat_syntax.SpaceURI("ats://did:plc:abc123/network.habitat.space/team")

	t.Run("user did", func(t *testing.T) {
		subject, err := parseCheckSubject(alice.String(), "")
		require.NoError(t, err)
		require.Equal(t, UserSubject{DID: alice}, subject)
	})

	t.Run("space role userset", func(t *testing.T) {
		subject, err := parseCheckSubject(space.String(), string(RoleReader))
		require.NoError(t, err)
		require.Equal(t, SpaceRoleSubject{Space: space, Role: RoleReader}, subject)
	})

	t.Run("user with subjectRole is rejected", func(t *testing.T) {
		_, err := parseCheckSubject(alice.String(), string(RoleReader))
		require.ErrorIs(t, err, ErrInvalidTuple)
	})

	t.Run("space without subjectRole is rejected", func(t *testing.T) {
		_, err := parseCheckSubject(space.String(), "")
		require.ErrorIs(t, err, ErrInvalidTuple)
	})

	t.Run("space with invalid subjectRole is rejected", func(t *testing.T) {
		_, err := parseCheckSubject(space.String(), "bogus")
		require.ErrorIs(t, err, ErrInvalidTuple)
	})

	t.Run("non-did non-space subject is rejected", func(t *testing.T) {
		_, err := parseCheckSubject("not-a-subject", "")
		require.ErrorIs(t, err, ErrInvalidTuple)
	})
}

func TestWriteTuple_UserReader(t *testing.T) {
	rel, sp := newTestStore(t)
	space := newSpace(t, sp, docsType, "doc")

	uri, err := rel.WriteTuple(t.Context(), UserSubject{DID: alice}, RoleReader, space)
	require.NoError(t, err)
	require.NotEmpty(t, uri)

	allowed, err := rel.Check(t.Context(), UserSubject{DID: alice}, RoleReader, space)
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, err = rel.Check(t.Context(), UserSubject{DID: alice}, RoleWriter, space)
	require.NoError(t, err)
	require.False(t, allowed)

	allowed, err = rel.Check(t.Context(), UserSubject{DID: bob}, RoleReader, space)
	require.NoError(t, err)
	require.False(t, allowed)
}

func TestWriteTuple_OwnerImpliesLowerRoles(t *testing.T) {
	rel, sp := newTestStore(t)
	space := newSpace(t, sp, docsType, "doc")

	_, err := rel.WriteTuple(t.Context(), UserSubject{DID: alice}, RoleOwner, space)
	require.NoError(t, err)

	for _, role := range []Role{RoleOwner, RoleManager, RoleWriter, RoleReader} {
		allowed, err := rel.Check(t.Context(), UserSubject{DID: alice}, role, space)
		require.NoError(t, err)
		require.True(t, allowed, "owner should imply %s", role)
	}
}

func TestWriteTuple_Idempotent(t *testing.T) {
	rel, sp := newTestStore(t)
	space := newSpace(t, sp, docsType, "doc")

	uri1, err := rel.WriteTuple(t.Context(), UserSubject{DID: alice}, RoleReader, space)
	require.NoError(t, err)
	uri2, err := rel.WriteTuple(t.Context(), UserSubject{DID: alice}, RoleReader, space)
	require.NoError(t, err)
	require.Equal(t, uri1, uri2)

	tuples, err := rel.ListTuples(t.Context(), space, ListTuplesFilter{})
	require.NoError(t, err)
	require.Len(t, tuples, 1)
}

func TestWriteTuple_InvalidRole(t *testing.T) {
	rel, sp := newTestStore(t)
	space := newSpace(t, sp, docsType, "doc")

	_, err := rel.WriteTuple(t.Context(), UserSubject{DID: alice}, Role("member"), space)
	require.ErrorIs(t, err, ErrInvalidTuple)
}

func TestWriteTuple_SpaceNotFound(t *testing.T) {
	rel, _ := newTestStore(t)
	missing := habitat_syntax.ConstructSpaceURI(org, docsType, "nope")

	_, err := rel.WriteTuple(t.Context(), UserSubject{DID: alice}, RoleReader, missing)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
}

func TestWriteTuple_RecordIsOrgOwnedAndReadable(t *testing.T) {
	rel, sp := newTestStore(t)
	space := newSpace(t, sp, docsType, "doc")

	_, err := rel.WriteTuple(t.Context(), UserSubject{DID: alice}, RoleReader, space)
	require.NoError(t, err)

	// The tuple record is stored under the org's repo, readable by other apps
	// via the generic space record API.
	collection := tupleCollection
	records, err := sp.ListRecords(t.Context(), space, org, &collection)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, org, records[0].Owner)
}

func TestDeleteTuple(t *testing.T) {
	rel, sp := newTestStore(t)
	space := newSpace(t, sp, docsType, "doc")

	uri, err := rel.WriteTuple(t.Context(), UserSubject{DID: alice}, RoleReader, space)
	require.NoError(t, err)

	require.NoError(t, rel.DeleteTuple(t.Context(), uri))

	allowed, err := rel.Check(t.Context(), UserSubject{DID: alice}, RoleReader, space)
	require.NoError(t, err)
	require.False(t, allowed)

	tuples, err := rel.ListTuples(t.Context(), space, ListTuplesFilter{})
	require.NoError(t, err)
	require.Empty(t, tuples)
}

func TestDeleteTuple_NotFound(t *testing.T) {
	rel, sp := newTestStore(t)
	space := newSpace(t, sp, docsType, "doc")

	missing := habitat_syntax.ConstructSpaceRecordURI(
		space, org, tupleCollection, syntax.RecordKey("3kdoesnotexist"),
	)
	require.ErrorIs(t, rel.DeleteTuple(t.Context(), missing), ErrTupleNotFound)

	require.ErrorIs(t, rel.DeleteTuple(t.Context(), "not-a-uri"), ErrTupleNotFound)
}

func TestListTuples_Filters(t *testing.T) {
	rel, sp := newTestStore(t)
	space := newSpace(t, sp, docsType, "doc")
	group := newSpace(t, sp, groupType, "team")

	_, err := rel.WriteTuple(t.Context(), UserSubject{DID: alice}, RoleReader, space)
	require.NoError(t, err)
	_, err = rel.WriteTuple(t.Context(), UserSubject{DID: bob}, RoleWriter, space)
	require.NoError(t, err)
	_, err = rel.WriteTuple(
		t.Context(),
		SpaceRoleSubject{Space: group, Role: RoleReader},
		RoleReader,
		space,
	)
	require.NoError(t, err)

	all, err := rel.ListTuples(t.Context(), space, ListTuplesFilter{})
	require.NoError(t, err)
	require.Len(t, all, 3)

	reader := RoleReader
	byRelation, err := rel.ListTuples(t.Context(), space, ListTuplesFilter{Relation: &reader})
	require.NoError(t, err)
	require.Len(t, byRelation, 2)

	byUser, err := rel.ListTuples(
		t.Context(),
		space,
		ListTuplesFilter{SubjectKind: SubjectKindUser},
	)
	require.NoError(t, err)
	require.Len(t, byUser, 2)

	bySpace, err := rel.ListTuples(
		t.Context(),
		space,
		ListTuplesFilter{SubjectKind: SubjectKindSpace},
	)
	require.NoError(t, err)
	require.Len(t, bySpace, 1)

	byDID, err := rel.ListTuples(t.Context(), space, ListTuplesFilter{SubjectDID: &alice})
	require.NoError(t, err)
	require.Len(t, byDID, 1)

	objFilter := space
	byObject, err := rel.ListTuples(t.Context(), space, ListTuplesFilter{Object: &objFilter})
	require.NoError(t, err)
	require.Len(t, byObject, 3)
}

// TestGroupAsSpace exercises the core "groups are spaces" model: a group-space's
// members (its readers) are granted a role on another space via a
// SpaceRoleSubject userset, and access resolves transitively.
func TestGroupAsSpace(t *testing.T) {
	rel, sp := newTestStore(t)
	group := newSpace(t, sp, groupType, "team")
	target := newSpace(t, sp, docsType, "doc")

	// alice is a member of the group (reader of the group-space).
	_, err := rel.WriteTuple(t.Context(), UserSubject{DID: alice}, RoleReader, group)
	require.NoError(t, err)

	// the group's members are writers of the target space.
	_, err = rel.WriteTuple(
		t.Context(),
		SpaceRoleSubject{Space: group, Role: RoleReader},
		RoleWriter,
		target,
	)
	require.NoError(t, err)

	allowed, err := rel.Check(t.Context(), UserSubject{DID: alice}, RoleWriter, target)
	require.NoError(t, err)
	require.True(t, allowed, "group member should be a writer of the target space")

	allowed, err = rel.Check(t.Context(), UserSubject{DID: bob}, RoleWriter, target)
	require.NoError(t, err)
	require.False(t, allowed, "non-member should not have access")

	// The userset itself can be checked directly: the group's readers are
	// writers of the target space.
	allowed, err = rel.Check(
		t.Context(),
		SpaceRoleSubject{Space: group, Role: RoleReader},
		RoleWriter,
		target,
	)
	require.NoError(t, err)
	require.True(t, allowed, "the group's readers should be writers of the target space")
}

func TestListSubjects(t *testing.T) {
	rel, sp := newTestStore(t)
	space := newSpace(t, sp, docsType, "doc")

	_, err := rel.WriteTuple(t.Context(), UserSubject{DID: alice}, RoleReader, space)
	require.NoError(t, err)
	_, err = rel.WriteTuple(t.Context(), UserSubject{DID: bob}, RoleWriter, space)
	require.NoError(t, err)

	readers, err := rel.ListSubjects(t.Context(), space, RoleReader)
	require.NoError(t, err)
	require.Contains(t, readers, alice)
	require.Contains(t, readers, bob) // writer implies reader

	writers, err := rel.ListSubjects(t.Context(), space, RoleWriter)
	require.NoError(t, err)
	require.Contains(t, writers, bob)
	require.NotContains(t, writers, alice)
}

func TestListObjects(t *testing.T) {
	rel, sp := newTestStore(t)
	spaceA := newSpace(t, sp, docsType, "a")
	spaceB := newSpace(t, sp, docsType, "b")

	_, err := rel.WriteTuple(t.Context(), UserSubject{DID: alice}, RoleReader, spaceA)
	require.NoError(t, err)
	_, err = rel.WriteTuple(t.Context(), UserSubject{DID: alice}, RoleWriter, spaceB)
	require.NoError(t, err)

	readers, err := rel.ListObjects(t.Context(), alice, RoleReader)
	require.NoError(t, err)
	require.Contains(t, readers, spaceA)
	require.Contains(t, readers, spaceB) // writer implies reader

	writers, err := rel.ListObjects(t.Context(), alice, RoleWriter)
	require.NoError(t, err)
	require.Contains(t, writers, spaceB)
	require.NotContains(t, writers, spaceA)
}

// flakyFGA wraps a real FGA store but can be told to fail mutating WriteRaw
// calls, to exercise transaction rollback.
type flakyFGA struct {
	fgastore.Store
	failWrites bool
}

func (f *flakyFGA) WriteRaw(ctx context.Context, req *openfgav1.WriteRequest) error {
	if f.failWrites {
		return errors.New("simulated fga failure")
	}
	return f.Store.WriteRaw(ctx, req)
}

func TestWriteTuple_RollsBackRecordOnFGAFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	require.NoError(t, err)
	mem, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = mem.Close() })
	fga := &flakyFGA{Store: mem}
	eventStore, err := events.NewStore(db)
	require.NoError(t, err)
	sp, err := spaces.NewStore(db, fga, eventStore)
	require.NoError(t, err)
	rel := NewStore(db, sp, fga)

	// CreateSpace uses fga.Write (not WriteRaw), so it succeeds.
	space := newSpace(t, sp, docsType, "doc")

	fga.failWrites = true
	_, err = rel.WriteTuple(t.Context(), UserSubject{DID: alice}, RoleReader, space)
	require.Error(t, err)

	// The FGA write failed, so the tuple record must have been rolled back.
	fga.failWrites = false
	tuples, err := rel.ListTuples(t.Context(), space, ListTuplesFilter{})
	require.NoError(t, err)
	require.Empty(t, tuples)

	allowed, err := rel.Check(t.Context(), UserSubject{DID: alice}, RoleReader, space)
	require.NoError(t, err)
	require.False(t, allowed)
}

func TestInvalidRole_QueryMethods(t *testing.T) {
	rel, sp := newTestStore(t)
	space := newSpace(t, sp, docsType, "doc")

	_, err := rel.Check(t.Context(), UserSubject{DID: alice}, Role("bogus"), space)
	require.ErrorIs(t, err, ErrInvalidTuple)

	_, err = rel.ListSubjects(t.Context(), space, Role("bogus"))
	require.ErrorIs(t, err, ErrInvalidTuple)

	_, err = rel.ListObjects(t.Context(), alice, Role("bogus"))
	require.ErrorIs(t, err, ErrInvalidTuple)
}
