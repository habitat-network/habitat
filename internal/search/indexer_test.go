package search

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	"github.com/habitat-network/habitat/internal/perms"
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
	mu      sync.Mutex
	docs    map[habitat_syntax.SpaceRecordURI]Document
	cursors map[RepoCursor]bool
	// puts receives a value after each Put, for tests that index in the
	// background.
	puts chan struct{}
}

func newFakeIndex() *fakeIndex {
	return &fakeIndex{
		docs:    map[habitat_syntax.SpaceRecordURI]Document{},
		cursors: map[RepoCursor]bool{},
		puts:    make(chan struct{}, 1000),
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
	for c := range f.cursors {
		if c.Space == space {
			delete(f.cursors, c)
		}
	}
	return nil
}

func (f *fakeIndex) SetSpaceAccess(
	_ context.Context,
	space habitat_syntax.SpaceURI,
	access Access,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for u, d := range f.docs {
		if d.Space == space {
			d.Access = access
			f.docs[u] = d
		}
	}
	return nil
}

func (f *fakeIndex) Search(context.Context, Query) (Result, error) {
	return Result{}, nil
}

func (f *fakeIndex) Cursor(
	_ context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for c := range f.cursors {
		if c.Space == space && c.Repo == repo {
			return c.Rev, nil
		}
	}
	return "", nil
}

func (f *fakeIndex) SetCursor(_ context.Context, cursor RepoCursor) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for c := range f.cursors {
		if c.Space == cursor.Space && c.Repo == cursor.Repo {
			delete(f.cursors, c)
		}
	}
	f.cursors[cursor] = true
	return nil
}

func (f *fakeIndex) Cursors(context.Context) ([]RepoCursor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []RepoCursor
	for c := range f.cursors {
		out = append(out, c)
	}
	return out, nil
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

// access returns the indexed access of a document.
func (f *fakeIndex) access(t *testing.T, uri habitat_syntax.SpaceRecordURI) Access {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.docs[uri]
	require.True(t, ok, "%s is not indexed", uri)
	return d.Access
}

// indexerTest is an indexer over a fake index, fed by a spaces store that
// notifies it, with a perms store over the same data.
type indexerTest struct {
	x      *Indexer
	idx    *fakeIndex
	store  spaces.Store
	perms  perms.Store
	db     *gorm.DB
	social *opensocial_testutil.TestStore
}

// setupIndexer returns an indexerTest. Nothing is indexed until the test
// drains or sweeps.
func setupIndexer(t *testing.T) *indexerTest {
	t.Helper()
	gdb := pear_testutil.NewPearDB(t)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })
	idx := newFakeIndex()
	x := NewIndexer(idx)
	store := spaces_testutil.NewTestStore(t,
		spaces_testutil.WithDB(gdb), spaces_testutil.WithFGA(fga), spaces_testutil.WithNotifier(x))
	social := opensocial_testutil.NewTestStore(t,
		opensocial_testutil.WithDB(gdb), opensocial_testutil.WithSpaceStore(store))
	return &indexerTest{
		x:      x,
		idx:    idx,
		store:  store,
		perms:  perms.NewStore(gdb, store, fga, social),
		db:     gdb,
		social: social,
	}
}

func (it *indexerTest) drain(ctx context.Context) {
	it.x.drain(ctx, it.store, it.perms)
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
	it := setupIndexer(t)
	ctx := t.Context()
	space, err := it.store.CreateSpace(ctx, testOrg, testType, "a")
	require.NoError(t, err)

	put(t, it.store, space, "k1", "first draft")
	put(t, it.store, space, "k2", "second note")
	it.drain(ctx)
	require.Equal(t, map[habitat_syntax.SpaceRecordURI]string{
		recordURI(space, "k1"): "first draft",
		recordURI(space, "k2"): "second note",
	}, it.idx.texts())

	put(t, it.store, space, "k1", "final draft")
	require.NoError(t, it.store.DeleteRecord(ctx, space, testRepo, testColl, "k2"))
	it.drain(ctx)
	require.Equal(t, map[habitat_syntax.SpaceRecordURI]string{
		recordURI(space, "k1"): "final draft",
	}, it.idx.texts())

	require.NoError(t, it.store.DeleteSpace(ctx, space))
	it.drain(ctx)
	require.Empty(t, it.idx.texts())
	cursors, err := it.idx.Cursors(ctx)
	require.NoError(t, err)
	require.Empty(t, cursors)
}

func TestIndexerPagesThroughRepo(t *testing.T) {
	it := setupIndexer(t)
	ctx := t.Context()
	space, err := it.store.CreateSpace(ctx, testOrg, testType, "a")
	require.NoError(t, err)

	writes := make([]spaces.Write, opsPageSize+5)
	for i := range writes {
		writes[i] = spaces.Write{
			Action:     spaces.WriteCreate,
			Collection: testColl,
			Value:      spaces_testutil.MustMarshalRecord(t, map[string]any{"text": "note"}),
		}
	}
	_, err = it.store.ApplyWrites(ctx, space, testRepo, writes)
	require.NoError(t, err)
	it.drain(ctx)
	require.Len(t, it.idx.texts(), len(writes))
}

func TestIndexerSkipsReservedCollections(t *testing.T) {
	it := setupIndexer(t)
	ctx := t.Context()
	space, err := it.store.CreateSpace(ctx, testOrg, testType, "a")
	require.NoError(t, err)

	_, _, err = it.store.PutRecord(ctx, space, testRepo, habitat_syntax.AppAccessCollection, "app",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"text": "not searchable"}))
	require.NoError(t, err)
	_, err = it.perms.SetUserRelation(ctx, "did:plc:bob", space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)
	put(t, it.store, space, "k1", "searchable")
	it.drain(ctx)
	require.Equal(t, map[habitat_syntax.SpaceRecordURI]string{
		recordURI(space, "k1"): "searchable",
	}, it.idx.texts())
}

func TestIndexerIndexesAccess(t *testing.T) {
	it := setupIndexer(t)
	ctx := t.Context()
	space, err := it.store.CreateSpace(ctx, testOrg, testType, "doc")
	require.NoError(t, err)
	team, err := it.store.CreateSpace(ctx, testOrg, testType, "team")
	require.NoError(t, err)
	put(t, it.store, space, "k1", "plans")
	it.drain(ctx)
	doc := recordURI(space, "k1")
	// The owner reads its own space.
	require.Equal(t, Access{Principals: Principals{Users: []syntax.DID{testOrg}}},
		it.idx.access(t, doc))

	t.Run("a userRelation adds a reader", func(t *testing.T) {
		_, err := it.perms.SetUserRelation(
			ctx,
			"did:plc:bob",
			space,
			habitat_syntax.SpaceRoleReader,
		)
		require.NoError(t, err)
		it.drain(ctx)
		require.ElementsMatch(t, []syntax.DID{testOrg, "did:plc:bob"},
			it.idx.access(t, doc).Users)
	})

	t.Run("a spaceRelation's users are flattened", func(t *testing.T) {
		_, err := it.perms.SetSpaceRoleRelation(
			ctx, team, habitat_syntax.SpaceRoleWriter, space, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
		it.drain(ctx)
		// Joining the team gives read access to the space.
		_, err = it.perms.SetUserRelation(
			ctx,
			"did:plc:carol",
			team,
			habitat_syntax.SpaceRoleWriter,
		)
		require.NoError(t, err)
		it.drain(ctx)
		require.ElementsMatch(t, []syntax.DID{testOrg, "did:plc:bob", "did:plc:carol"},
			it.idx.access(t, doc).Users)
	})

	t.Run("revoking removes the reader", func(t *testing.T) {
		require.NoError(t, it.perms.RevokeUser(ctx, "did:plc:bob", space))
		it.drain(ctx)
		require.ElementsMatch(t, []syntax.DID{testOrg, "did:plc:carol"},
			it.idx.access(t, doc).Users)
	})

	t.Run("an opensocial access record adds community roles", func(t *testing.T) {
		_, _, err := it.store.PutRecord(ctx, space, testOrg, accessCollection, "self",
			spaces_testutil.MustMarshalRecord(t, map[string]any{
				"$type": accessCollection.String(),
				"roles": []any{"staff", "board"},
			}))
		require.NoError(t, err)
		it.drain(ctx)
		require.Equal(t, []CommunityRole{
			{Community: testOrg, Role: "staff"}, {Community: testOrg, Role: "board"},
		}, it.idx.access(t, doc).CommunityRoles)
	})

	t.Run("an about space is public", func(t *testing.T) {
		about, err := it.store.CreateSpace(ctx, testOrg, opensocial.AboutSpaceType, "self")
		require.NoError(t, err)
		put(t, it.store, about, "k1", "hello")
		it.drain(ctx)
		require.Equal(t, Access{Public: true}, it.idx.access(t, recordURI(about, "k1")))
	})
}

func TestIndexerSweepCatchesUp(t *testing.T) {
	it := setupIndexer(t)
	ctx := t.Context()
	space, err := it.store.CreateSpace(ctx, testOrg, testType, "a")
	require.NoError(t, err)
	gone, err := it.store.CreateSpace(ctx, testOrg, testType, "gone")
	require.NoError(t, err)
	put(t, it.store, space, "k1", "kept")
	put(t, it.store, gone, "k1", "deleted with its space")
	it.drain(ctx)
	require.Len(t, it.idx.texts(), 2)

	// Changes whose notifications never reach the indexer, as when pear
	// stopped before indexing them.
	unnotified := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(it.db))
	put(t, unnotified, space, "k2", "missed")
	require.NoError(t, unnotified.DeleteSpace(ctx, gone))
	it.drain(ctx)
	require.Len(t, it.idx.texts(), 2)

	it.x.sweep(ctx, it.store)
	it.drain(ctx)
	require.Equal(t, map[habitat_syntax.SpaceRecordURI]string{
		recordURI(space, "k1"): "kept",
		recordURI(space, "k2"): "missed",
	}, it.idx.texts())

	// Once caught up, a sweep finds nothing to do.
	it.x.sweep(ctx, it.store)
	_, pending := it.x.next()
	require.False(t, pending)
}

func TestIndexerRunIndexesUntilCancelled(t *testing.T) {
	it := setupIndexer(t)
	ctx, cancel := context.WithCancel(t.Context())
	space, err := it.store.CreateSpace(ctx, testOrg, testType, "a")
	require.NoError(t, err)
	// Written before Run starts, so only the startup sweep finds it.
	put(t, it.store, space, "k1", "before")

	done := make(chan error, 1)
	go func() { done <- it.x.Run(ctx, it.store, it.perms) }()
	it.idx.waitForPut(t)
	require.Len(t, it.idx.texts(), 1)

	put(t, it.store, space, "k2", "after")
	it.idx.waitForPut(t)
	require.Len(t, it.idx.texts(), 2)

	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}
