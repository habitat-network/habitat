package main

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func userTuple(object, did, relation string) tupleRow {
	return tupleRow{
		RecordURI:   object + "/" + did + "/" + relation,
		ObjectSpace: object,
		Relation:    relation,
		SubjectKind: "user",
		SubjectDID:  did,
	}
}

func groupTuple(object, subjectGroup, subjectRole, relation string) tupleRow {
	return tupleRow{
		RecordURI:    object + "/" + subjectGroup,
		ObjectSpace:  object,
		Relation:     relation,
		SubjectKind:  "group",
		SubjectGroup: subjectGroup,
		SubjectRole:  subjectRole,
	}
}

func memberDIDs(members []member) []string {
	dids := make([]string, len(members))
	for i, m := range members {
		dids[i] = m.DID
	}
	sort.Strings(dids)
	return dids
}

func TestHolders_DirectUserIsMember(t *testing.T) {
	g := newGraph([]tupleRow{userTuple("groupA", "did:web:alice", "writer")})
	require.True(t, g.isMember("groupA", "did:web:alice"))
	require.False(t, g.isMember("groupA", "did:web:bob"))
}

func TestHolders_ReaderIsNotMember(t *testing.T) {
	g := newGraph([]tupleRow{userTuple("groupA", "did:web:alice", "reader")})
	require.False(t, g.isMember("groupA", "did:web:alice"))
}

func TestHolders_ManagerImpliesMemberAndManage(t *testing.T) {
	g := newGraph([]tupleRow{userTuple("groupA", "did:web:alice", "manager")})
	require.True(t, g.isMember("groupA", "did:web:alice"))
	require.True(t, g.canManage("groupA", "did:web:alice"))
}

func TestHolders_WriterCannotManage(t *testing.T) {
	g := newGraph([]tupleRow{userTuple("groupA", "did:web:alice", "writer")})
	require.False(t, g.canManage("groupA", "did:web:alice"))
}

func TestHolders_InheritedGroupMembersAreMembers(t *testing.T) {
	// groupA inherits groupB's writers; alice is a direct writer of groupB.
	g := newGraph([]tupleRow{
		userTuple("groupB", "did:web:alice", "writer"),
		groupTuple("groupA", "groupB", "writer", "writer"),
	})

	require.True(t, g.isMember("groupA", "did:web:alice"))

	members := g.holders("groupA", memberMinRole)
	require.Len(t, members, 1)
	require.Equal(t, "did:web:alice", members[0].DID)
	require.False(t, members[0].Direct)
	require.Equal(t, "groupB", members[0].ViaGroup)

	require.Equal(t, []string{"groupB"}, g.inheritedGroups("groupA"))
}

func TestHolders_DirectPreferredOverInherited(t *testing.T) {
	g := newGraph([]tupleRow{
		userTuple("groupB", "did:web:alice", "writer"),
		groupTuple("groupA", "groupB", "writer", "writer"),
		userTuple("groupA", "did:web:alice", "writer"),
	})
	members := g.holders("groupA", memberMinRole)
	require.Len(t, members, 1)
	require.True(t, members[0].Direct)
}

func TestHolders_TransitiveInheritanceAndCycleSafe(t *testing.T) {
	// A inherits B, B inherits A (cycle), plus alice in B and bob in A.
	g := newGraph([]tupleRow{
		userTuple("groupB", "did:web:alice", "writer"),
		userTuple("groupA", "did:web:bob", "writer"),
		groupTuple("groupA", "groupB", "writer", "writer"),
		groupTuple("groupB", "groupA", "writer", "writer"),
	})
	require.Equal(
		t,
		[]string{"did:web:alice", "did:web:bob"},
		memberDIDs(g.holders("groupA", memberMinRole)),
	)
	require.Equal(
		t,
		[]string{"did:web:alice", "did:web:bob"},
		memberDIDs(g.holders("groupB", memberMinRole)),
	)
}
