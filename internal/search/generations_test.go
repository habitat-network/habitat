package search_test

import (
	"context"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/meilisearch/meilisearch-go"
	"github.com/stretchr/testify/require"

	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"
	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/fgastore"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/search/searchtest"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	genLockKey   db.LockKey = 7
	buildLockKey db.LockKey = 8
	// genInterval keeps tests from waiting on the production check interval.
	genInterval = 50 * time.Millisecond
	genTimeout  = 30 * time.Second
)

// genTest is a spaces store with a space in it, and what Generations runs
// against.
type genTest struct {
	prefix string
	lock   *db.Lock
	// buildLock is what builds hold.
	buildLock *db.Lock
	store     spaces.Store
	perms     perms.Store
	space     habitat_syntax.SpaceURI
	// notify forwards the store's notifications to whichever Generations
	// the test runs.
	notify *forwardingNotifier
}

// forwardingNotifier passes notifications on to the Generations a test
// opened last.
type forwardingNotifier struct {
	spaces.Notifiers
}

func setupGenerations(t *testing.T) *genTest {
	t.Helper()
	gdb := pear_testutil.NewPearDB(t)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })
	notify := &forwardingNotifier{}
	store := spaces_testutil.NewTestStore(
		t,
		spaces_testutil.WithDB(
			gdb,
		),
		spaces_testutil.WithFGA(fga),
		spaces_testutil.WithNotifier(notify),
	)
	social := opensocial_testutil.NewTestStore(t,
		opensocial_testutil.WithDB(gdb), opensocial_testutil.WithSpaceStore(store))
	space, err := store.CreateSpace(t.Context(), searchOrg, searchType, "a")
	require.NoError(t, err)
	return &genTest{
		prefix:    searchtest.NewUID(t),
		lock:      db.NewLock(gdb, genLockKey),
		buildLock: db.NewLock(gdb, buildLockKey),
		store:     store,
		perms:     perms.NewStore(gdb, store, fga, social),
		space:     space,
		notify:    notify,
	}
}

// open returns the Generations of the test's index, forwarding the store's
// writes to it.
func (gt *genTest) open(t *testing.T, opts ...search.GenerationsOption) *search.Generations {
	t.Helper()
	opts = append([]search.GenerationsOption{search.WithCheckInterval(genInterval)}, opts...)
	g, err := search.NewGenerations(t.Context(), searchtest.URL(t), "", gt.prefix, gt.lock, opts...)
	require.NoError(t, err)
	gt.notify.Notifiers = spaces.Notifiers{g}
	return g
}

// builder returns Generations for building, with opts, as a `pear run
// reindex` process would.
func (gt *genTest) builder(
	t *testing.T,
	opts ...search.GenerationsOption,
) *search.Generations {
	t.Helper()
	opts = append([]search.GenerationsOption{search.WithCheckInterval(genInterval)}, opts...)
	g, err := search.NewGenerations(t.Context(), searchtest.URL(t), "", gt.prefix, gt.lock, opts...)
	require.NoError(t, err)
	return g
}

// build builds with b and returns the generation that became current.
func (gt *genTest) build(t *testing.T, b *search.Generations, fresh bool) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), genTimeout)
	defer cancel()
	n, err := b.Build(ctx, gt.buildLock, gt.store, gt.perms, fresh)
	require.NoError(t, err)
	require.Equal(t, n, b.Current())
	return n
}

// run runs g until the returned stop is called.
func (gt *genTest) run(t *testing.T, g *search.Generations) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- g.Run(ctx, gt.store, gt.perms) }()
	return func() {
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
	}
}

func (gt *genTest) put(t *testing.T, rkey, text string) {
	t.Helper()
	_, _, err := gt.store.PutRecord(t.Context(), gt.space, searchRepo, searchColl,
		syntax.RecordKey(rkey), spaces_testutil.MustMarshalRecord(t, map[string]any{"text": text}))
	require.NoError(t, err)
}

func (gt *genTest) uri(rkey string) habitat_syntax.SpaceRecordURI {
	return habitat_syntax.ConstructSpaceRecordURI(gt.space, searchRepo, searchColl,
		syntax.RecordKey(rkey))
}

// found returns the URIs of the records g finds for text.
func (gt *genTest) found(
	t *testing.T,
	g search.Searchable,
	text string,
) []habitat_syntax.SpaceRecordURI {
	t.Helper()
	res, err := g.Search(t.Context(), search.Query{
		Text: text, Spaces: []habitat_syntax.SpaceURI{gt.space},
	})
	require.NoError(t, err)
	out := []habitat_syntax.SpaceRecordURI{}
	for _, h := range res.Hits {
		out = append(out, h.URI)
	}
	return out
}

func requireCurrent(t *testing.T, g *search.Generations, n int) {
	t.Helper()
	require.Eventually(t, func() bool { return g.Current() == n }, genTimeout, genInterval,
		"generation %d never became current; current is %d", n, g.Current())
}

func indexExists(t *testing.T, uid string) bool {
	t.Helper()
	client := meilisearch.New(searchtest.URL(t))
	_, err := client.GetIndexWithContext(t.Context(), uid)
	if err == nil {
		return true
	}
	var apiErr *meilisearch.Error
	require.ErrorAs(t, err, &apiErr)
	return false
}

func TestGenerationsBuildsTheFirstGenerationFromScratch(t *testing.T) {
	gt := setupGenerations(t)
	gt.put(t, "k1", "quarterly budget")
	g := gt.open(t)
	require.Equal(t, -1, g.Current())
	require.Empty(t, gt.found(t, g, "budget"), "searched before there was an index")

	stop := gt.run(t, g)
	defer stop()
	requireCurrent(t, g, 1)
	require.Eventually(t, func() bool {
		return len(gt.found(t, g, "budget")) == 1
	}, genTimeout, genInterval)

	gt.put(t, "k2", "budget review")
	require.Eventually(t, func() bool {
		return len(gt.found(t, g, "budget")) == 2
	}, genTimeout, genInterval, "a write after the build was not indexed")
}

func TestGenerationsBuildReplacesTheIndexWhenItsSpecChanges(t *testing.T) {
	gt := setupGenerations(t)
	ctx := t.Context()
	// The index from before generations, holding a document the store
	// doesn't have, so it can be told apart from a rebuilt one.
	legacy := searchtest.OpenIndex(t, gt.prefix)
	require.NoError(t, legacy.Put(ctx, search.Document{
		URI:        gt.uri("old"),
		Space:      gt.space,
		Repo:       searchRepo,
		Collection: searchColl,
		Rev:        "3kaaaaaaaaaa2",
		Text:       "budget from the old index",
		Access:     search.Access{Public: true},
	}))
	gt.put(t, "k1", "quarterly budget")

	g := gt.open(t)
	require.Equal(t, 0, g.Current())
	stop := gt.run(t, g)
	defer stop()
	// A second process can't take the lock, so it only follows.
	follower, err := search.NewGenerations(ctx, searchtest.URL(t), "", gt.prefix, gt.lock,
		search.WithCheckInterval(genInterval))
	require.NoError(t, err)
	stopFollower := gt.run(t, follower)
	defer stopFollower()
	// Running doesn't rebuild the index from before generations.
	require.Equal(t, []habitat_syntax.SpaceRecordURI{gt.uri("old")}, gt.found(t, g, "budget"))

	require.Equal(t, 1, gt.build(t, gt.builder(t), false))
	requireCurrent(t, g, 1)
	requireCurrent(t, follower, 1)
	require.Equal(t, []habitat_syntax.SpaceRecordURI{gt.uri("k1")}, gt.found(t, g, "budget"))
	require.Equal(t, []habitat_syntax.SpaceRecordURI{gt.uri("k1")}, gt.found(t, follower, "budget"))
	// The running process indexes writes into the new current generation.
	gt.put(t, "k2", "budget review")
	require.Eventually(t, func() bool {
		return len(gt.found(t, g, "budget")) == 2
	}, genTimeout, genInterval, "a write after the flip was not indexed")
	// The old index is kept for rollback.
	require.True(t, indexExists(t, gt.prefix))

	// With nothing changed there is nothing to build.
	require.Equal(t, 1, gt.build(t, gt.builder(t), false))
	require.False(t, indexExists(t, gt.prefix+"_g2"))

	// Settings with an embedder build generation 2, and the index from before
	// generations falls out of the kept range.
	embedder := search.WithIndexOptions(search.WithEmbedder(searchtest.Embedder(t, 0.5, 0)))
	require.Equal(t, 2, gt.build(t, gt.builder(t, embedder), false))
	requireCurrent(t, g, 2)
	require.False(t, indexExists(t, gt.prefix))
	require.True(t, indexExists(t, gt.prefix+"_g1"))

	// Building with the old settings again, as after a rollback, catches the
	// kept generation 1 up instead of building a new one, and drops
	// generation 2.
	gt.put(t, "k3", "budget written during the rollback")
	require.Equal(t, 1, gt.build(t, gt.builder(t), false))
	requireCurrent(t, g, 1)
	require.Len(t, gt.found(t, g, "budget"), 3)
	require.False(t, indexExists(t, gt.prefix+"_g2"))

	// A fresh build rebuilds even though nothing changed.
	n := gt.build(t, gt.builder(t), true)
	require.Equal(t, 2, n)
	requireCurrent(t, g, n)
	require.Len(t, gt.found(t, g, "budget"), 3)
}

func TestGenerationsBuildRunsOneAtATime(t *testing.T) {
	gt := setupGenerations(t)
	_, release, ok, err := gt.buildLock.TryAcquire(t.Context())
	require.NoError(t, err)
	require.True(t, ok)
	defer release()
	_, err = gt.builder(t).Build(t.Context(), gt.buildLock, gt.store, gt.perms, false)
	require.ErrorIs(t, err, search.ErrBuildRunning)
}
