package search_test

import (
	"context"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/meilisearch/meilisearch-go"
	"github.com/stretchr/testify/require"

	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"
	"github.com/habitat-network/habitat/internal/fgastore"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/search/searchtest"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// reindexTimeout bounds a reindex in tests.
const reindexTimeout = 30 * time.Second

// reindexTest is a spaces store with a space in it, and an index name to
// reindex.
type reindexTest struct {
	uid   string
	store spaces.Store
	perms perms.Store
	space habitat_syntax.SpaceURI
}

func setupReindex(t *testing.T) *reindexTest {
	t.Helper()
	gdb := pear_testutil.NewPearDB(t)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })
	store := spaces_testutil.NewTestStore(t,
		spaces_testutil.WithDB(gdb), spaces_testutil.WithFGA(fga))
	social := opensocial_testutil.NewTestStore(t,
		opensocial_testutil.WithDB(gdb), opensocial_testutil.WithSpaceStore(store))
	space, err := store.CreateSpace(t.Context(), searchOrg, searchType, "a")
	require.NoError(t, err)
	return &reindexTest{
		uid:   searchtest.NewUID(t),
		store: store,
		perms: perms.NewStore(gdb, store, fga, social),
		space: space,
	}
}

func (rt *reindexTest) put(t *testing.T, rkey, text string) {
	t.Helper()
	_, _, err := rt.store.PutRecord(t.Context(), rt.space, searchRepo, searchColl,
		syntax.RecordKey(rkey), spaces_testutil.MustMarshalRecord(t, map[string]any{"text": text}))
	require.NoError(t, err)
}

func (rt *reindexTest) uri(rkey string) habitat_syntax.SpaceRecordURI {
	return habitat_syntax.ConstructSpaceRecordURI(rt.space, searchRepo, searchColl,
		syntax.RecordKey(rkey))
}

func (rt *reindexTest) reindex(t *testing.T, opts ...search.MeilisearchOption) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), reindexTimeout)
	defer cancel()
	r := search.NewReindexer(searchtest.URL(t), "", rt.uid,
		search.WithReindexIndexOptions(opts...),
		search.WithReindexCheckInterval(50*time.Millisecond))
	require.NoError(t, r.Reindex(ctx, rt.store, rt.perms, nil))
}

// found returns the URIs of the records index finds for text.
func (rt *reindexTest) found(
	t *testing.T,
	index search.Index,
	text string,
) []habitat_syntax.SpaceRecordURI {
	t.Helper()
	res, err := index.Search(t.Context(), search.Query{
		Text: text, Spaces: []habitat_syntax.SpaceURI{rt.space},
	})
	require.NoError(t, err)
	out := []habitat_syntax.SpaceRecordURI{}
	for _, h := range res.Hits {
		out = append(out, h.URI)
	}
	return out
}

func indexExists(t *testing.T, uid string) bool {
	t.Helper()
	_, err := meilisearch.New(searchtest.URL(t)).GetIndexWithContext(t.Context(), uid)
	if err == nil {
		return true
	}
	var apiErr *meilisearch.Error
	require.ErrorAs(t, err, &apiErr)
	return false
}

func hasEmbedder(t *testing.T, uid string) bool {
	t.Helper()
	embedders, err := meilisearch.New(searchtest.URL(t)).Index(uid).
		GetEmbeddersWithContext(t.Context())
	require.NoError(t, err)
	return len(embedders) > 0
}

func TestReindexerSwapsInARebuiltIndex(t *testing.T) {
	rt := setupReindex(t)
	ctx := t.Context()
	embedder := search.WithEmbedder(searchtest.Embedder(t, 0.5, 0))
	// The live index holds a document the store doesn't have, so it can be
	// told apart from a rebuilt one.
	live, err := search.OpenMeilisearch(ctx, searchtest.URL(t), "", rt.uid)
	require.NoError(t, err)
	require.NoError(t, live.Put(ctx, search.Document{
		URI:        rt.uri("old"),
		Space:      rt.space,
		Repo:       searchRepo,
		Collection: searchColl,
		Rev:        "3kaaaaaaaaaa2",
		Text:       "budget from the old index",
		Access:     search.Access{Public: true},
	}))
	rt.put(t, "k1", "quarterly budget")

	// Opening the index with an embedder leaves its settings alone.
	live, err = search.OpenMeilisearch(ctx, searchtest.URL(t), "", rt.uid, embedder)
	require.NoError(t, err)
	require.False(t, hasEmbedder(t, rt.uid))
	require.Equal(t, []habitat_syntax.SpaceRecordURI{rt.uri("old")}, rt.found(t, live, "budget"))

	rt.reindex(t, embedder)
	require.True(t, hasEmbedder(t, rt.uid))
	// Searches through the index opened before the swap read the new one.
	require.Equal(t, []habitat_syntax.SpaceRecordURI{rt.uri("k1")}, rt.found(t, live, "budget"))
	require.False(t, indexExists(t, rt.uid+"_next"))
	require.False(t, indexExists(t, rt.uid+"_next_cursors"))

	// The swapped in cursors are the new index's, so indexing resumes from
	// them rather than redoing the repo.
	cursor, err := live.Cursor(ctx, rt.space, searchRepo)
	require.NoError(t, err)
	require.NotEmpty(t, cursor)

	// Reindexing without the embedder drops it.
	rt.reindex(t)
	require.False(t, hasEmbedder(t, rt.uid))
	require.Equal(t, []habitat_syntax.SpaceRecordURI{rt.uri("k1")}, rt.found(t, live, "budget"))
}

func TestReindexerBuildsAMissingIndex(t *testing.T) {
	rt := setupReindex(t)
	rt.put(t, "k1", "quarterly budget")
	rt.reindex(t)
	idx, err := search.OpenMeilisearch(t.Context(), searchtest.URL(t), "", rt.uid)
	require.NoError(t, err)
	require.Equal(t, []habitat_syntax.SpaceRecordURI{rt.uri("k1")}, rt.found(t, idx, "budget"))
	require.False(t, indexExists(t, rt.uid+"_next"))
}
