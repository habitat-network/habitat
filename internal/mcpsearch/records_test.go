package mcpsearch

import (
	"context"
	"errors"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// fakeRecordSearcher records the last search it ran and returns canned matches.
type fakeRecordSearcher struct {
	caller  syntax.DID
	query   search.Query
	matches []search.Match
	err     error
}

func (f *fakeRecordSearcher) Search(
	_ context.Context, caller syntax.DID, q search.Query,
) (search.Page, error) {
	f.caller, f.query = caller, q
	return search.Page{Matches: f.matches}, f.err
}

const testRecordURI = habitat_syntax.SpaceRecordURI(
	"at://did:plc:org/space/network.habitat.test/a/did:plc:user/network.habitat.note/1",
)

func cannedMatch() search.Match {
	return search.Match{
		URI: testRecordURI,
		Record: spaces.Record{
			Space: "at://did:plc:org/space/network.habitat.test/a",
			Value: map[string]any{"k": "v"},
		},
		Snippet: "<mark>roadmap</mark>",
	}
}

func TestRecordsBackendSearchesAsCaller(t *testing.T) {
	records := &fakeRecordSearcher{matches: []search.Match{cannedMatch()}}
	out := NewRecordsBackend(records).Search(t.Context(), Request{
		Caller: testCaller, Org: testOrg,
		Query: Query{
			Text:          "roadmap",
			Limit:         5,
			SnippetLength: 30,
			Collection:    "network.habitat.note",
		},
	})

	require.Equal(t, testCaller, records.caller)
	require.Equal(t, search.Query{
		Text: "roadmap", Org: testOrg, Limit: 5, SnippetLength: 30,
		Collections: []syntax.NSID{"network.habitat.note"},
	}, records.query)
	require.Equal(t, []Source{{Provider: ProviderRecords, Status: StatusOK, Count: 1}}, out.Sources)
	require.Len(t, out.Items, 1)
	require.Equal(t, testRecordURI.String(), out.Items[0].URL)
	require.Equal(t, ProviderRecords, out.Items[0].Source.Provider)
	require.Equal(t, map[string]any{"k": "v"}, out.Items[0].Record.Value)
	require.Equal(t, "<mark>roadmap</mark>", out.Items[0].Snippet)
}

func TestRecordsBackendReportsFailure(t *testing.T) {
	out := NewRecordsBackend(&fakeRecordSearcher{err: errors.New("index down")}).Search(
		t.Context(), Request{Caller: testCaller, Org: testOrg, Query: Query{Text: "x"}},
	)
	require.Empty(t, out.Items)
	require.Equal(t, []Source{
		{Provider: ProviderRecords, Status: StatusError, Error: "index down"},
	}, out.Sources)
}

func TestSearcherSearchesRecordsAndServersTogether(t *testing.T) {
	alpha := newFakeSearchServer(t, "alpha", nil)
	servers := newFakeConnectedServers()
	servers.connect("alpha", alpha)
	records := &fakeRecordSearcher{matches: []search.Match{cannedMatch()}}
	searcher := NewSearcher(
		servers, NewRegistry(testProvider("alpha-search", alpha, "search")),
		WithBackend(NewRecordsBackend(records)), WithMaxLimit(100),
	)

	res, err := searcher.Search(
		t.Context(),
		testCaller,
		testOrg,
		nil,
		Query{Text: "roadmap", Limit: 80},
	)
	require.NoError(t, err)
	// Records come first, then each server; a server is asked for no more
	// than the most any tool accepts.
	require.Equal(t, []string{"", "alpha-roadmap-0"}, titles(res.Items)[:2])
	require.Equal(t, 80, records.query.Limit)
	require.Equal(t, []Source{
		{Provider: ProviderRecords, Status: StatusOK, Count: 1},
		{
			ServerURL:  alpha,
			ServerName: "alpha",
			Provider:   "alpha-search",
			Status:     StatusOK,
			Count:      MaxLimit,
		},
	}, res.Sources)

	// Only records have collections.
	res, err = searcher.Search(t.Context(), testCaller, testOrg, nil, Query{
		Text: "roadmap", Collection: "network.habitat.note",
	})
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	require.Len(t, res.Sources, 1)

	// The limit is the searcher's, not MaxLimit.
	_, err = searcher.Search(t.Context(), testCaller, testOrg, nil, Query{Text: "x", Limit: 101})
	require.ErrorIs(t, err, ErrInvalidLimit)
}

func TestSearcherWithoutServers(t *testing.T) {
	records := &fakeRecordSearcher{matches: []search.Match{cannedMatch()}}
	res, err := NewSearcher(nil, NewRegistry(), WithBackend(NewRecordsBackend(records))).Search(
		t.Context(), testCaller, testOrg, nil, Query{Text: "roadmap"},
	)
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
}
