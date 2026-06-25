package main

import (
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestStore(t *testing.T) *PostStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/greensky.db"), &gorm.Config{})
	require.NoError(t, err)
	store, err := NewPostStore(db)
	require.NoError(t, err)
	return store
}

// rootURI and replyURI build distinct SpaceRecordURIs within a thread's space.
func rootURI(space, author, rkey string) string {
	return space + "/" + author + "/network.habitat.greensky.post/" + rkey
}

func TestThreadsForAuthorOrdersAndGroups(t *testing.T) {
	store := newTestStore(t)
	author := syntax.DID("did:web:alice.example")
	other := syntax.DID("did:web:bob.example")

	spaceA := "ats://did:web:org.example/network.habitat.greensky.thread/aaa"
	spaceB := "ats://did:web:org.example/network.habitat.greensky.thread/bbb"

	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	// Two root posts by alice in separate spaces, plus a reply in space A.
	mustUpsert(t, store, post{
		URI:      rootURI(spaceA, author.String(), "r1"),
		SpaceURI: spaceA, Author: author.String(), Text: "first", PostedAt: base,
	})
	mustUpsert(t, store, post{
		URI:      rootURI(spaceB, author.String(), "r2"),
		SpaceURI: spaceB, Author: author.String(), Text: "second", PostedAt: base.Add(time.Hour),
	})
	mustUpsert(t, store, post{
		URI:         rootURI(spaceA, author.String(), "reply1"),
		SpaceURI:    spaceA,
		Author:      author.String(),
		Text:        "self reply",
		PostedAt:    base.Add(2 * time.Hour),
		ReplyParent: rootURI(spaceA, author.String(), "r1"),
		ReplyRoot:   rootURI(spaceA, author.String(), "r1"),
	})
	// A post by someone else must not appear in alice's threads.
	mustUpsert(t, store, post{
		URI:      rootURI(spaceB, other.String(), "x1"),
		SpaceURI: spaceB, Author: other.String(), Text: "not mine", PostedAt: base,
	})

	threads, err := store.ThreadsForAuthor(t.Context(), author)
	require.NoError(t, err)
	require.Len(t, threads, 2)

	// Newest root first.
	require.Equal(t, "second", threads[0].Root.Text)
	require.Empty(t, threads[0].Replies)

	require.Equal(t, "first", threads[1].Root.Text)
	require.Len(t, threads[1].Replies, 1)
	require.Equal(t, "self reply", threads[1].Replies[0].Text)
}

func TestUpsertReplacesByURI(t *testing.T) {
	store := newTestStore(t)
	author := syntax.DID("did:web:alice.example")
	space := "ats://did:web:org.example/network.habitat.greensky.thread/aaa"
	uri := rootURI(space, author.String(), "r1")

	mustUpsert(
		t,
		store,
		post{URI: uri, SpaceURI: space, Author: author.String(), Text: "v1", PostedAt: time.Now()},
	)
	mustUpsert(
		t,
		store,
		post{URI: uri, SpaceURI: space, Author: author.String(), Text: "v2", PostedAt: time.Now()},
	)

	threads, err := store.ThreadsForAuthor(t.Context(), author)
	require.NoError(t, err)
	require.Len(t, threads, 1)
	require.Equal(t, "v2", threads[0].Root.Text)
}

func mustUpsert(t *testing.T, store *PostStore, p post) {
	t.Helper()
	require.NoError(t, store.Upsert(t.Context(), p))
}
