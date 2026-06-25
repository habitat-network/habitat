package relationship

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func testGroupURI(t *testing.T) habitat_syntax.SpaceRecordURI {
	t.Helper()
	space := habitat_syntax.ConstructSpaceURI(orgDID, "network.habitat.space", "a")
	return habitat_syntax.ConstructSpaceRecordURI(
		space,
		orgDID,
		groupNSID,
		"g1",
	)
}

func TestSubjectRoundTrip(t *testing.T) {
	group := testGroupURI(t)
	space := habitat_syntax.ConstructSpaceURI(orgDID, "network.habitat.space", "a")
	cases := []Subject{
		UserSubject{DID: "did:plc:alice"},
		GroupSubject{Group: group},
		SpaceRoleSubject{Space: space, Role: RoleWriter},
		OrgRoleSubject{Org: orgDID, Role: RoleMember},
	}
	for _, want := range cases {
		got, err := ParseSubject(subjectValue(want))
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}

func TestObjectRoundTrip(t *testing.T) {
	group := testGroupURI(t)
	space := habitat_syntax.ConstructSpaceURI(orgDID, "network.habitat.space", "a")
	cases := []Object{
		SpaceObject{Space: space},
		GroupObject{Group: group},
	}
	for _, want := range cases {
		got, err := ParseObject(objectValue(want))
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}

func TestParseSubjectErrors(t *testing.T) {
	_, err := ParseSubject("not-a-map")
	require.ErrorIs(t, err, ErrInvalidTuple)

	_, err = ParseSubject(map[string]any{"did": "did:plc:alice"})
	require.ErrorIs(t, err, ErrInvalidTuple, "missing $type")

	_, err = ParseSubject(map[string]any{"$type": "unknown#thing"})
	require.ErrorIs(t, err, ErrInvalidTuple)

	_, err = ParseSubject(map[string]any{"$type": typeUserSubject})
	require.ErrorIs(t, err, ErrInvalidTuple, "missing did")

	_, err = ParseSubject(map[string]any{"$type": typeUserSubject, "did": 42})
	require.ErrorIs(t, err, ErrInvalidTuple, "non-string did")

	_, err = ParseSubject(map[string]any{"$type": typeUserSubject, "did": "nope"})
	require.ErrorIs(t, err, ErrInvalidTuple, "invalid did")

	_, err = ParseSubject(map[string]any{"$type": typeGroupSubject, "group": "bad-uri"})
	require.ErrorIs(t, err, ErrInvalidTuple)
}

func TestParseObjectErrors(t *testing.T) {
	_, err := ParseObject(42)
	require.ErrorIs(t, err, ErrInvalidTuple)

	_, err = ParseObject(map[string]any{"$type": "unknown#thing"})
	require.ErrorIs(t, err, ErrInvalidTuple)

	_, err = ParseObject(map[string]any{"$type": typeSpaceObject, "space": "bad"})
	require.ErrorIs(t, err, ErrInvalidTuple)
}

func TestTranslationValidation(t *testing.T) {
	// orgRoleRelation accepts only admin|member.
	rel, err := orgRoleRelation(RoleAdmin)
	require.NoError(t, err)
	require.Equal(t, "admin", rel)
	_, err = orgRoleRelation(RoleWriter)
	require.ErrorIs(t, err, ErrInvalidTuple)

	// A space role subject with an invalid role fails.
	_, err = subjectToFGAUser(SpaceRoleSubject{Space: "ats://did:plc:o/t/s", Role: "bogus"})
	require.ErrorIs(t, err, ErrInvalidTuple)

	// member is not valid on a space; a non-member is not valid on a group.
	_, _, err = resolveObject(RoleMember, SpaceObject{Space: "ats://did:plc:o/t/s"})
	require.ErrorIs(t, err, ErrInvalidTuple)
	_, _, err = resolveObject(RoleWriter, GroupObject{Group: testGroupURI(t)})
	require.ErrorIs(t, err, ErrInvalidTuple)

	// governingSpace rejects a malformed group URI.
	_, err = governingSpace(GroupObject{Group: "not-a-record-uri"})
	require.ErrorIs(t, err, ErrInvalidTuple)
}

func TestListObjectsAndObjectFilter(t *testing.T) {
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

	spaceURIs, err := s.ListObjects(t.Context(), alice, RoleWriter)
	require.NoError(t, err)
	require.Contains(t, spaceURIs, space)

	byObject, err := s.ListTuples(t.Context(), space, TupleFilter{ObjectURI: space.String()})
	require.NoError(t, err)
	require.Len(t, byObject, 1)

	none, err := s.ListTuples(t.Context(), space, TupleFilter{ObjectURI: "ats://did:plc:o/t/other"})
	require.NoError(t, err)
	require.Empty(t, none)
}
