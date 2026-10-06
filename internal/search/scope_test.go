package search_test

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// captureIndex records the last query it was asked to run.
type captureIndex struct {
	search.Index
	last search.Query
}

func (c *captureIndex) Search(_ context.Context, q search.Query) (search.Result, error) {
	c.last = q
	return search.Result{}, nil
}

// noRecords is a RecordGetter for tests whose index returns no hits.
type noRecords struct{}

func (noRecords) GetRecords(
	context.Context,
	[]habitat_syntax.SpaceRecordURI,
) ([]spaces.Record, error) {
	return nil, nil
}

type fakeCollections map[syntax.DID][]syntax.NSID

func (fakeCollections) Defaults() []syntax.NSID { return []syntax.NSID{"com.example.default"} }

func (f fakeCollections) Collections(_ context.Context, org syntax.DID) ([]syntax.NSID, error) {
	return f[org], nil
}

func TestSearcher_LimitsToConfiguredCollections(t *testing.T) {
	member := syntax.DID("did:plc:member")
	orgA := syntax.DID("did:plc:orga")
	orgB := syntax.DID("did:plc:orgb")
	idx := &captureIndex{}
	s := search.NewSearcher(idx, fakeCommunities{}, noRecords{},
		search.WithCollections(fakeCollections{
			orgA: {"com.example.a"},
			orgB: {"com.example.b"},
		}))
	run := func(q search.Query) search.Query {
		t.Helper()
		_, err := s.Search(t.Context(), member, q)
		require.NoError(t, err)
		return idx.last
	}

	t.Run("defaults plus the org's collections", func(t *testing.T) {
		got := run(search.Query{Text: "x", Org: orgA})
		require.Equal(t, orgA, got.Org)
		require.ElementsMatch(t,
			[]syntax.NSID{"com.example.default", "com.example.a"}, got.Collections)
	})

	t.Run("a collection filter narrows further", func(t *testing.T) {
		got := run(search.Query{
			Text: "x", Org: orgA, Collections: []syntax.NSID{"com.example.a", "com.example.b"},
		})
		require.Equal(t, []syntax.NSID{"com.example.a"}, got.Collections)
	})

	t.Run("a collection filter outside the allowed set finds nothing", func(t *testing.T) {
		idx.last = search.Query{}
		page, err := s.Search(t.Context(), member, search.Query{
			Text: "x", Org: orgA, Collections: []syntax.NSID{"com.example.b"},
		})
		require.NoError(t, err)
		require.Empty(t, page.Matches)
		// The index wasn't queried.
		require.Empty(t, idx.last.Text)
	})

	t.Run("requires an org", func(t *testing.T) {
		_, err := s.Search(t.Context(), member, search.Query{Text: "x"})
		require.ErrorIs(t, err, search.ErrOrgRequired)
	})

	t.Run("a space implies its owner for a credential", func(t *testing.T) {
		_, err := s.SearchSpaces(t.Context(), search.Query{
			Text:   "x",
			Spaces: []habitat_syntax.SpaceURI{habitat_syntax.ConstructSpaceURI(orgB, "t", "k")},
		})
		require.NoError(t, err)
		require.Equal(t, orgB, idx.last.Org)
		require.ElementsMatch(t,
			[]syntax.NSID{"com.example.default", "com.example.b"}, idx.last.Collections)
	})
}

func TestSearcher_WithoutCollectionSourceIsUnlimited(t *testing.T) {
	idx := &captureIndex{}
	s := search.NewSearcher(idx, fakeCommunities{}, noRecords{})
	_, err := s.Search(t.Context(), "did:plc:member", search.Query{Text: "x"})
	require.NoError(t, err)
	require.Empty(t, idx.last.Collections)
}
