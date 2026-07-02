package main

import (
	"context"
	"sort"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/stretchr/testify/require"
)

// recordURI builds a valid space-record URI for the given space, repo,
// collection and rkey.
func recordURI(space, repo, collection, rkey string) habitat_syntax.SpaceRecordURI {
	return habitat_syntax.ConstructSpaceRecordURI(
		habitat_syntax.SpaceURI(space),
		syntax.DID(repo),
		syntax.NSID(collection),
		syntax.RecordKey(rkey),
	)
}

const (
	space1 = "ats://did:web:org/network.habitat.space/spaceone"
	space2 = "ats://did:web:org/network.habitat.space/spacetwo"
)

// seedRecords indexes a small fixture: record A lives in both spaces, B only in
// space1, C only in space2.
func seedRecords(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()
	uris := []habitat_syntax.SpaceRecordURI{
		recordURI(space1, "did:web:alice", "app.bsky.feed.post", "r1"), // A
		recordURI(space2, "did:web:alice", "app.bsky.feed.post", "r1"), // A (shared)
		recordURI(space1, "did:web:bob", "app.bsky.feed.post", "r2"),   // B
		recordURI(space2, "did:web:alice", "app.bsky.feed.like", "r3"), // C
	}
	for _, u := range uris {
		require.NoError(t, store.UpsertRecord(ctx, u))
	}
}

func TestCountCollectionsDeduplicatesAcrossSpaces(t *testing.T) {
	ctx := context.Background()
	store := setupStore(t)
	seedRecords(t, ctx, store)

	counts, err := store.CountCollections(ctx, []string{space1, space2})
	require.NoError(t, err)

	got := map[string]int64{}
	for _, c := range counts {
		got[c.Collection] = c.Count
	}
	// Record A is in both spaces but counts once.
	require.Equal(t, map[string]int64{
		"app.bsky.feed.post": 2,
		"app.bsky.feed.like": 1,
	}, got)
}

func TestCountCollectionsFiltersBySpace(t *testing.T) {
	ctx := context.Background()
	store := setupStore(t)
	seedRecords(t, ctx, store)

	counts, err := store.CountCollections(ctx, []string{space1})
	require.NoError(t, err)
	got := map[string]int64{}
	for _, c := range counts {
		got[c.Collection] = c.Count
	}
	// space1 has posts A and B, and no likes.
	require.Equal(t, map[string]int64{"app.bsky.feed.post": 2}, got)

	// No readable spaces means nothing is visible.
	empty, err := store.CountCollections(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, empty)
}

func TestListRecordsInSpacesFiltersByCollectionAndSpace(t *testing.T) {
	ctx := context.Background()
	store := setupStore(t)
	seedRecords(t, ctx, store)

	rows, err := store.ListRecordsInSpaces(ctx, []string{space1, space2}, "app.bsky.feed.post")
	require.NoError(t, err)
	// A (x2 spaces) + B (x1) = 3 rows, none from the like collection.
	require.Len(t, rows, 3)
	for _, r := range rows {
		require.Equal(t, "app.bsky.feed.post", r.Collection)
	}

	// Restricting to space2 drops record B, which only lives in space1.
	rows, err = store.ListRecordsInSpaces(ctx, []string{space2}, "app.bsky.feed.post")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "at://did:web:alice/app.bsky.feed.post/r1", rows[0].AtURI)

	// No readable spaces yields nothing.
	rows, err = store.ListRecordsInSpaces(ctx, nil, "app.bsky.feed.post")
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestDeleteRecordRemovesOnlyOneSpaceCopy(t *testing.T) {
	ctx := context.Background()
	store := setupStore(t)
	seedRecords(t, ctx, store)

	// Deleting record A's copy in space1 leaves its space2 copy indexed.
	require.NoError(t, store.DeleteRecord(
		ctx, recordURI(space1, "did:web:alice", "app.bsky.feed.post", "r1"),
	))
	rows, err := store.ListRecordsInSpaces(ctx, []string{space1, space2}, "app.bsky.feed.post")
	require.NoError(t, err)
	require.Len(t, rows, 2) // A in space2, B in space1
}

func TestUpsertRecordIgnoresMalformedURI(t *testing.T) {
	ctx := context.Background()
	store := setupStore(t)

	// Missing the repo/collection/rkey suffix: not a space-record URI.
	require.NoError(t, store.UpsertRecord(ctx, habitat_syntax.SpaceRecordURI(space1)))
	rows, err := store.ListRecordsInSpaces(ctx, []string{space1}, "app.bsky.feed.post")
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestGroupRecordViewsCollapsesSpaces(t *testing.T) {
	rows := []recordRow{
		{AtURI: "at://did:web:alice/c/r1", Repo: "did:web:alice", Collection: "c", Rkey: "r1", SpaceURI: space1},
		{AtURI: "at://did:web:alice/c/r1", Repo: "did:web:alice", Collection: "c", Rkey: "r1", SpaceURI: space2},
		{AtURI: "at://did:web:bob/c/r2", Repo: "did:web:bob", Collection: "c", Rkey: "r2", SpaceURI: space1},
	}
	views := groupRecordViews(rows)
	require.Len(t, views, 2)

	byURI := map[string][]string{}
	for _, v := range views {
		spaces := append([]string(nil), v.Spaces...)
		sort.Strings(spaces)
		byURI[v.Uri] = spaces
	}
	require.Equal(t, []string{space1, space2}, byURI["at://did:web:alice/c/r1"])
	require.Equal(t, []string{space1}, byURI["at://did:web:bob/c/r2"])
}
