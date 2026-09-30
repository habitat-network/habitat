package search_test

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	searchOrg  = syntax.DID("did:plc:org")
	searchRepo = syntax.DID("did:plc:alice")
	searchType = syntax.NSID("network.habitat.group")
	searchColl = syntax.NSID("network.habitat.note")
)

// fakeSpaceLister lists the spaces each DID can read.
type fakeSpaceLister map[syntax.DID][]habitat_syntax.SpaceURI

func (f fakeSpaceLister) ListObjects(
	_ context.Context,
	did syntax.DID,
	_ habitat_syntax.SpaceRole,
	_ *syntax.NSID,
) ([]habitat_syntax.SpaceURI, error) {
	return f[did], nil
}

// setupSearcher returns a searcher over a fresh index and spaces store, which
// lets each DID read the spaces the returned lister maps it to.
func setupSearcher(t *testing.T) (search.Index, spaces.Store, fakeSpaceLister, *search.Searcher) {
	t.Helper()
	gdb := pear_testutil.NewPearDB(t)
	idx, err := search.New(gdb)
	require.NoError(t, err)
	store := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(gdb))
	lister := fakeSpaceLister{}
	return idx, store, lister, search.NewSearcher(idx, lister, store)
}

// putIndexed writes a record to the store and its text to the index.
func putIndexed(
	t *testing.T,
	idx search.Index,
	store spaces.Store,
	space habitat_syntax.SpaceURI,
	rkey syntax.RecordKey,
	text string,
) habitat_syntax.SpaceRecordURI {
	t.Helper()
	value := map[string]any{"text": text}
	uri, _, err := store.PutRecord(t.Context(), space, searchRepo, searchColl, rkey,
		spaces_testutil.MustMarshalRecord(t, value))
	require.NoError(t, err)
	require.NoError(t, idx.Put(t.Context(), search.Document{
		URI:        uri,
		Space:      space,
		Repo:       searchRepo,
		Collection: searchColl,
		Rev:        syntax.NewTIDNow(0),
		Text:       search.ExtractText(value),
	}))
	return uri
}

func matchURIs(page search.Page) []habitat_syntax.SpaceRecordURI {
	uris := []habitat_syntax.SpaceRecordURI{}
	for _, m := range page.Matches {
		uris = append(uris, m.URI)
	}
	return uris
}

func TestSearcherScopesToReadableSpaces(t *testing.T) {
	idx, store, lister, searcher := setupSearcher(t)
	ctx := t.Context()
	readable, err := store.CreateSpace(ctx, searchOrg, searchType, "readable")
	require.NoError(t, err)
	hidden, err := store.CreateSpace(ctx, searchOrg, searchType, "hidden")
	require.NoError(t, err)
	readableURI := putIndexed(t, idx, store, readable, "k1", "apple pie")
	hiddenURI := putIndexed(t, idx, store, hidden, "k1", "apple tart")
	lister[searchRepo] = []habitat_syntax.SpaceURI{readable}

	page, err := searcher.Search(ctx, searchRepo, search.Query{Text: "apple"})
	require.NoError(t, err)
	require.Equal(t, []habitat_syntax.SpaceRecordURI{readableURI}, matchURIs(page))
	match := page.Matches[0]
	require.Equal(t, readable, match.Record.Space)
	require.Equal(t, map[string]any{"text": "apple pie"}, match.Record.Value)
	require.Contains(t, match.Snippet, "<mark>apple</mark>")

	// Asking for a space the reader can't read finds nothing in it.
	page, err = searcher.Search(ctx, searchRepo,
		search.Query{Text: "apple", Spaces: []habitat_syntax.SpaceURI{hidden}})
	require.NoError(t, err)
	require.Empty(t, page.Matches)

	// Nor does a reader who can read nothing.
	page, err = searcher.Search(ctx, "did:plc:nobody", search.Query{Text: "apple"})
	require.NoError(t, err)
	require.Empty(t, page.Matches)

	// SearchSpaces trusts the caller to have authorized the spaces.
	page, err = searcher.SearchSpaces(ctx,
		search.Query{Text: "apple", Spaces: []habitat_syntax.SpaceURI{hidden}})
	require.NoError(t, err)
	require.Equal(t, []habitat_syntax.SpaceRecordURI{hiddenURI}, matchURIs(page))
}

func TestSearcherDropsRecordsDeletedSinceIndexing(t *testing.T) {
	idx, store, lister, searcher := setupSearcher(t)
	ctx := t.Context()
	space, err := store.CreateSpace(ctx, searchOrg, searchType, "a")
	require.NoError(t, err)
	kept := putIndexed(t, idx, store, space, "k1", "apple pie")
	putIndexed(t, idx, store, space, "k2", "apple tart")
	lister[searchRepo] = []habitat_syntax.SpaceURI{space}
	// Deleted from the store, but not yet from the index.
	require.NoError(t, store.DeleteRecord(ctx, space, searchRepo, searchColl, "k2"))

	page, err := searcher.Search(ctx, searchRepo, search.Query{Text: "apple"})
	require.NoError(t, err)
	require.Equal(t, []habitat_syntax.SpaceRecordURI{kept}, matchURIs(page))
}
