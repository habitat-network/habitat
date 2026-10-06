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

func (f fakeCollections) ListAll(context.Context) (map[syntax.DID][]syntax.NSID, error) {
	return f, nil
}

func TestSearcher_ScopesToConfiguredCollections(t *testing.T) {
	member := syntax.DID("did:plc:member")
	orgA := syntax.DID("did:plc:orga")
	orgB := syntax.DID("did:plc:orgb")
	collections := fakeCollections{
		orgA: {"com.example.a"},
		orgB: {"com.example.b"},
	}
	want := &search.CollectionScope{
		Default: []syntax.NSID{"com.example.default"},
		ByOwner: map[syntax.DID][]syntax.NSID(collections),
	}
	idx := &captureIndex{}
	s := search.NewSearcher(idx, fakeCommunities{}, noRecords{},
		search.WithCollections(collections))

	// Every org's configuration applies, whether or not the caller belongs.
	_, err := s.Search(t.Context(), member, search.Query{Text: "x"})
	require.NoError(t, err)
	require.Equal(t, want, idx.last.Scope)

	_, err = s.SearchSpaces(t.Context(), search.Query{
		Text:   "x",
		Spaces: []habitat_syntax.SpaceURI{habitat_syntax.ConstructSpaceURI(orgB, "t", "k")},
	})
	require.NoError(t, err)
	require.Equal(t, want, idx.last.Scope)

	// Without a CollectionSource nothing is scoped.
	plain := search.NewSearcher(idx, fakeCommunities{}, noRecords{})
	_, err = plain.Search(t.Context(), member, search.Query{Text: "x"})
	require.NoError(t, err)
	require.Nil(t, idx.last.Scope)
}
