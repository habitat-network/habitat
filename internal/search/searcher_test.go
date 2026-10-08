package search_test

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/search/searchtest"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	searchOrg  = syntax.DID("did:plc:org")
	searchRepo = syntax.DID("did:plc:writer")
	searchType = syntax.NSID("network.habitat.group")
	searchColl = syntax.NSID("network.habitat.note")
)

// fakeCommunities holds each user's roles in each community.
type fakeCommunities map[syntax.DID]map[syntax.DID][]string

func (f fakeCommunities) ListMemberSpaces(
	_ context.Context,
	user syntax.DID,
) ([]habitat_syntax.SpaceURI, error) {
	var out []habitat_syntax.SpaceURI
	for community := range f[user] {
		out = append(out, habitat_syntax.ConstructSpaceURI(
			community, "community.opensocial.members", "self"))
	}
	return out, nil
}

func (f fakeCommunities) GetUserRoles(
	_ context.Context,
	community syntax.DID,
	user syntax.DID,
) ([]string, error) {
	return f[user][community], nil
}

// setupSearcher returns a searcher over a fresh index and spaces store, and
// the fake that holds users' community roles.
func setupSearcher(t *testing.T) (search.Index, spaces.Store, fakeCommunities, *search.Searcher) {
	t.Helper()
	idx := searchtest.NewIndex(t)
	store := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(pear_testutil.NewPearDB(t)))
	communities := fakeCommunities{}
	return idx, store, communities, search.NewSearcher(idx, communities, store)
}

// putIndexed writes a record to the store and its text to the index, readable
// per access.
func putIndexed(
	t *testing.T,
	idx search.Index,
	store spaces.Store,
	space habitat_syntax.SpaceURI,
	rkey syntax.RecordKey,
	text string,
	access search.Access,
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
		Access:     access,
	}))
	return uri
}

func readers(users ...syntax.DID) search.Access {
	return search.Access{Principals: search.Principals{Users: users}}
}

func matchURIs(page search.Page) []habitat_syntax.SpaceRecordURI {
	uris := []habitat_syntax.SpaceRecordURI{}
	for _, m := range page.Matches {
		uris = append(uris, m.URI)
	}
	return uris
}

func TestSearcherSearchesWhatTheCallerCanRead(t *testing.T) {
	idx, store, communities, searcher := setupSearcher(t)
	ctx := t.Context()
	a, err := store.CreateSpace(ctx, searchOrg, searchType, "a")
	require.NoError(t, err)
	b, err := store.CreateSpace(ctx, searchOrg, searchType, "b")
	require.NoError(t, err)
	byUser := putIndexed(t, idx, store, a, "k1", "apple pie", readers("did:plc:alice"))
	byRole := putIndexed(t, idx, store, b, "k1", "apple tart", search.Access{
		Principals: search.Principals{
			CommunityRoles: []search.CommunityRole{{Community: searchOrg, Role: "staff"}},
		},
	})
	communities["did:plc:alice"] = map[syntax.DID][]string{searchOrg: {"staff"}}
	communities["did:plc:bob"] = map[syntax.DID][]string{searchOrg: {"guest"}}

	page, err := searcher.Search(ctx, "did:plc:alice", search.Query{Text: "apple"})
	require.NoError(t, err)
	require.ElementsMatch(t, []habitat_syntax.SpaceRecordURI{byUser, byRole}, matchURIs(page))
	for _, m := range page.Matches {
		if m.URI == byUser {
			require.Equal(t, a, m.Record.Space)
			require.Equal(t, map[string]any{"text": "apple pie"}, m.Record.Value)
			require.Contains(t, m.Snippet, "<mark>apple</mark>")
		}
	}

	page, err = searcher.Search(ctx, "did:plc:alice",
		search.Query{Text: "apple", Spaces: []habitat_syntax.SpaceURI{b}})
	require.NoError(t, err)
	require.Equal(t, []habitat_syntax.SpaceRecordURI{byRole}, matchURIs(page))

	page, err = searcher.Search(ctx, "did:plc:bob", search.Query{Text: "apple"})
	require.NoError(t, err)
	require.Empty(t, page.Matches)

	// SearchSpaces trusts the caller to have authorized the spaces, whatever
	// reader the query names.
	page, err = searcher.SearchSpaces(ctx, search.Query{
		Text:   "apple",
		Spaces: []habitat_syntax.SpaceURI{a},
		Reader: &search.Reader{Users: []syntax.DID{"did:plc:bob"}},
	})
	require.NoError(t, err)
	require.Equal(t, []habitat_syntax.SpaceRecordURI{byUser}, matchURIs(page))
}

func TestSearcherDropsRecordsDeletedSinceIndexing(t *testing.T) {
	idx, store, _, searcher := setupSearcher(t)
	ctx := t.Context()
	space, err := store.CreateSpace(ctx, searchOrg, searchType, "a")
	require.NoError(t, err)
	kept := putIndexed(t, idx, store, space, "k1", "apple pie", readers("did:plc:alice"))
	putIndexed(t, idx, store, space, "k2", "apple tart", readers("did:plc:alice"))
	// Deleted from the store, but not yet from the index.
	require.NoError(t, store.DeleteRecord(ctx, space, searchRepo, searchColl, "k2"))

	page, err := searcher.Search(ctx, "did:plc:alice", search.Query{Text: "apple"})
	require.NoError(t, err)
	require.Equal(t, []habitat_syntax.SpaceRecordURI{kept}, matchURIs(page))
}
