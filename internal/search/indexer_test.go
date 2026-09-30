package search

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	testOrg  = syntax.DID("did:plc:org")
	testRepo = syntax.DID("did:plc:alice")
	testType = syntax.NSID("network.habitat.group")
	testColl = syntax.NSID("network.habitat.note")
)

// fakeIndex is an in-memory [Index] holding the latest document per URI.
type fakeIndex struct {
	mu   sync.Mutex
	docs map[habitat_syntax.SpaceRecordURI]Document
	// puts receives a value after each Put, for tests that index in the
	// background.
	puts chan struct{}
}

func newFakeIndex() *fakeIndex {
	return &fakeIndex{
		docs: map[habitat_syntax.SpaceRecordURI]Document{},
		puts: make(chan struct{}, 1000),
	}
}

// waitForPut blocks until the next Put.
func (f *fakeIndex) waitForPut(t *testing.T) {
	t.Helper()
	select {
	case <-f.puts:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the index to be written")
	}
}

func (f *fakeIndex) Put(_ context.Context, docs ...Document) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, d := range docs {
		if old, ok := f.docs[d.URI]; !ok || old.Rev < d.Rev {
			f.docs[d.URI] = d
		}
	}
	f.puts <- struct{}{}
	return nil
}

func (f *fakeIndex) Delete(_ context.Context, uris ...habitat_syntax.SpaceRecordURI) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range uris {
		delete(f.docs, u)
	}
	return nil
}

func (f *fakeIndex) DeleteSpace(_ context.Context, space habitat_syntax.SpaceURI) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for u, d := range f.docs {
		if d.Space == space {
			delete(f.docs, u)
		}
	}
	return nil
}

func (f *fakeIndex) Search(context.Context, Query) (Result, error) {
	return Result{}, nil
}

// texts returns the indexed text of each document, by URI.
func (f *fakeIndex) texts() map[habitat_syntax.SpaceRecordURI]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[habitat_syntax.SpaceRecordURI]string{}
	for u, d := range f.docs {
		out[u] = d.Text
	}
	return out
}

// setupIndexer returns an indexer over a fake index, and a spaces store that
// notifies it. Nothing is indexed until the test drains or sweeps.
func setupIndexer(t *testing.T) (*Indexer, *fakeIndex, spaces.Store) {
	t.Helper()
	x, idx, store, _ := setupIndexerDB(t)
	return x, idx, store
}

// setupIndexerDB is setupIndexer that also returns the database the store and
// the indexer's cursors live in.
func setupIndexerDB(t *testing.T) (*Indexer, *fakeIndex, spaces.Store, *gorm.DB) {
	t.Helper()
	gdb := db_testutil.NewDB(t, spaces.Models(), Models())
	idx := newFakeIndex()
	x := NewIndexer(gdb, idx)
	store := spaces_testutil.NewTestStore(t,
		spaces_testutil.WithDB(gdb), spaces_testutil.WithNotifier(x))
	return x, idx, store, gdb
}

func put(t *testing.T, store spaces.Store, space habitat_syntax.SpaceURI, rkey, text string) {
	t.Helper()
	_, _, err := store.PutRecord(t.Context(), space, testRepo, testColl, syntax.RecordKey(rkey),
		spaces_testutil.MustMarshalRecord(t, map[string]any{"text": text}))
	require.NoError(t, err)
}

func recordURI(space habitat_syntax.SpaceURI, rkey string) habitat_syntax.SpaceRecordURI {
	return habitat_syntax.ConstructSpaceRecordURI(space, testRepo, testColl, syntax.RecordKey(rkey))
}

func TestIndexerFollowsWritesAndDeletes(t *testing.T) {
	x, idx, store := setupIndexer(t)
	ctx := t.Context()
	space, err := store.CreateSpace(ctx, testOrg, testType, "a")
	require.NoError(t, err)

	put(t, store, space, "k1", "first draft")
	put(t, store, space, "k2", "second note")
	x.drain(ctx, store)
	require.Equal(t, map[habitat_syntax.SpaceRecordURI]string{
		recordURI(space, "k1"): "first draft",
		recordURI(space, "k2"): "second note",
	}, idx.texts())

	put(t, store, space, "k1", "final draft")
	require.NoError(t, store.DeleteRecord(ctx, space, testRepo, testColl, "k2"))
	x.drain(ctx, store)
	require.Equal(t, map[habitat_syntax.SpaceRecordURI]string{
		recordURI(space, "k1"): "final draft",
	}, idx.texts())

	require.NoError(t, store.DeleteSpace(ctx, space))
	x.drain(ctx, store)
	require.Empty(t, idx.texts())
	var cursors int64
	require.NoError(t, x.db.Model(&searchCursor{}).Count(&cursors).Error)
	require.Zero(t, cursors)
}

func TestIndexerPagesThroughRepo(t *testing.T) {
	x, idx, store := setupIndexer(t)
	ctx := t.Context()
	space, err := store.CreateSpace(ctx, testOrg, testType, "a")
	require.NoError(t, err)

	writes := make([]spaces.Write, opsPageSize+5)
	for i := range writes {
		writes[i] = spaces.Write{
			Action:     spaces.WriteCreate,
			Collection: testColl,
			Value:      spaces_testutil.MustMarshalRecord(t, map[string]any{"text": "note"}),
		}
	}
	_, err = store.ApplyWrites(ctx, space, testRepo, writes)
	require.NoError(t, err)
	x.drain(ctx, store)
	require.Len(t, idx.texts(), len(writes))
}

func TestIndexerSkipsReservedCollections(t *testing.T) {
	x, idx, store := setupIndexer(t)
	ctx := t.Context()
	space, err := store.CreateSpace(ctx, testOrg, testType, "a")
	require.NoError(t, err)

	_, _, err = store.PutRecord(ctx, space, testRepo, habitat_syntax.AppAccessCollection, "app",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"text": "not searchable"}))
	require.NoError(t, err)
	put(t, store, space, "k1", "searchable")
	x.drain(ctx, store)
	require.Equal(t, map[habitat_syntax.SpaceRecordURI]string{
		recordURI(space, "k1"): "searchable",
	}, idx.texts())
}

func TestIndexerSweepCatchesUp(t *testing.T) {
	x, idx, store, gdb := setupIndexerDB(t)
	ctx := t.Context()
	space, err := store.CreateSpace(ctx, testOrg, testType, "a")
	require.NoError(t, err)
	gone, err := store.CreateSpace(ctx, testOrg, testType, "gone")
	require.NoError(t, err)
	put(t, store, space, "k1", "kept")
	put(t, store, gone, "k1", "deleted with its space")
	x.drain(ctx, store)
	require.Len(t, idx.texts(), 2)

	// Changes whose notifications never reach the indexer, as when pear
	// stopped before indexing them.
	unnotified := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(gdb))
	put(t, unnotified, space, "k2", "missed")
	require.NoError(t, unnotified.DeleteSpace(ctx, gone))
	x.drain(ctx, store)
	require.Len(t, idx.texts(), 2)

	x.sweep(ctx, store)
	x.drain(ctx, store)
	require.Equal(t, map[habitat_syntax.SpaceRecordURI]string{
		recordURI(space, "k1"): "kept",
		recordURI(space, "k2"): "missed",
	}, idx.texts())

	// Once caught up, a sweep finds nothing to do.
	x.sweep(ctx, store)
	_, pending := x.next()
	require.False(t, pending)
}

func TestIndexerRunIndexesUntilCancelled(t *testing.T) {
	x, idx, store := setupIndexer(t)
	ctx, cancel := context.WithCancel(t.Context())
	space, err := store.CreateSpace(ctx, testOrg, testType, "a")
	require.NoError(t, err)
	// Written before Run starts, so only the startup sweep finds it.
	put(t, store, space, "k1", "before")

	done := make(chan error, 1)
	go func() { done <- x.Run(ctx, store) }()
	idx.waitForPut(t)
	require.Len(t, idx.texts(), 1)

	put(t, store, space, "k2", "after")
	idx.waitForPut(t)
	require.Len(t, idx.texts(), 2)

	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}
