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
	owners []syntax.DID
	last   search.Query
}

func (c *captureIndex) SpaceOwners(context.Context, search.Reader) ([]syntax.DID, error) {
	return c.owners, nil
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

func (f fakeCollections) Collections(
	_ context.Context,
	orgs ...syntax.DID,
) (map[syntax.DID][]syntax.NSID, error) {
	out := map[syntax.DID][]syntax.NSID{}
	for _, o := range orgs {
		if c, ok := f[o]; ok {
			out[o] = c
		}
	}
	return out, nil
}

func TestSearcher_ScopesToConfiguredCollections(t *testing.T) {
	member := syntax.DID("did:plc:member")
	orgA := syntax.DID("did:plc:orga")
	orgB := syntax.DID("did:plc:orgb")
	collections := fakeCollections{
		orgA: {"com.example.a"},
		orgB: {"com.example.b"},
	}
	// The caller can read spaces owned by orgA only.
	idx := &captureIndex{owners: []syntax.DID{orgA}}
	s := search.NewSearcher(idx, fakeCommunities{}, noRecords{},
		search.WithCollections(collections))

	// The configuration of the owners of spaces the caller can read applies.
	_, err := s.Search(t.Context(), member, search.Query{Text: "x"})
	require.NoError(t, err)
	require.Equal(t, &search.CollectionScope{
		Default: []syntax.NSID{"com.example.default"},
		ByOwner: map[syntax.DID][]syntax.NSID{orgA: {"com.example.a"}},
	}, idx.last.Scope)

	_, err = s.SearchSpaces(t.Context(), search.Query{
		Text:   "x",
		Spaces: []habitat_syntax.SpaceURI{habitat_syntax.ConstructSpaceURI(orgB, "t", "k")},
	})
	require.NoError(t, err)
	require.Equal(t, map[syntax.DID][]syntax.NSID{orgB: {"com.example.b"}}, idx.last.Scope.ByOwner)

	// Without a CollectionSource nothing is scoped.
	plain := search.NewSearcher(idx, fakeCommunities{}, noRecords{})
	_, err = plain.Search(t.Context(), member, search.Query{Text: "x"})
	require.NoError(t, err)
	require.Nil(t, idx.last.Scope)
}
