package main

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/stretchr/testify/require"
)

func setupStore(t *testing.T) *Store {
	t.Helper()
	db := testutil.NewDB(t)
	store, err := NewStore(db)
	require.NoError(t, err)
	return store
}

func TestFindUserTuples(t *testing.T) {
	ctx := context.Background()
	store := setupStore(t)

	const group = "space://did:web:org/network.habitat.group/abc"
	alice := syntax.DID("did:web:alice")

	require.NoError(t, store.UpsertTuple(ctx, tupleRow{
		RecordURI:   group + "/alice/writer",
		ObjectSpace: group,
		Relation:    "writer",
		SubjectKind: "user",
		SubjectDID:  alice.String(),
	}))
	// A tuple for a different user in the same group must not match.
	require.NoError(t, store.UpsertTuple(ctx, tupleRow{
		RecordURI:   group + "/bob/writer",
		ObjectSpace: group,
		Relation:    "writer",
		SubjectKind: "user",
		SubjectDID:  "did:web:bob",
	}))

	rows, err := store.FindUserTuples(ctx, habitat_syntax.SpaceURI(group), alice)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, group+"/alice/writer", rows[0].RecordURI)

	// Unknown user yields no rows (deleteMember treats this as MemberNotFound).
	rows, err = store.FindUserTuples(ctx, habitat_syntax.SpaceURI(group), "did:web:carol")
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestFindGroupTuples(t *testing.T) {
	ctx := context.Background()
	store := setupStore(t)

	const group = "space://did:web:org/network.habitat.group/abc"
	const inherited = "space://did:web:org/network.habitat.group/xyz"

	require.NoError(t, store.UpsertTuple(ctx, tupleRow{
		RecordURI:    group + "/xyz",
		ObjectSpace:  group,
		Relation:     "writer",
		SubjectKind:  "group",
		SubjectGroup: inherited,
		SubjectRole:  "writer",
	}))

	rows, err := store.FindGroupTuples(
		ctx, habitat_syntax.SpaceURI(group), habitat_syntax.SpaceURI(inherited),
	)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, group+"/xyz", rows[0].RecordURI)

	// A user tuple for the same URI-shaped subject must not match a group query.
	rows, err = store.FindGroupTuples(
		ctx, habitat_syntax.SpaceURI(group), "space://did:web:org/network.habitat.group/none",
	)
	require.NoError(t, err)
	require.Empty(t, rows)
}
