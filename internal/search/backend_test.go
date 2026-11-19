package search

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/fedsearch"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// fakeCallerSearcher answers every search with page, or fails with err, and
// remembers the last search.
type fakeCallerSearcher struct {
	page   Page
	err    error
	caller syntax.DID
	query  Query
}

func (f *fakeCallerSearcher) Search(_ context.Context, caller syntax.DID, q Query) (Page, error) {
	f.caller, f.query = caller, q
	return f.page, f.err
}

func TestResolverBackendSearchesAsCaller(t *testing.T) {
	ctx := t.Context()
	space := habitat_syntax.ConstructSpaceURI("did:plc:owner", "com.example.type", "key1")
	uri := habitat_syntax.ConstructSpaceRecordURI(space, "did:plc:repo", "com.example.note", "r1")
	fake := &fakeCallerSearcher{page: Page{Matches: []Match{
		{
			URI: uri,
			Record: spaces.Record{
				Space:      space,
				Collection: "com.example.note",
				Value:      map[string]any{"text": "hi"},
			},
			Snippet: "say <mark>hi</mark>",
		},
	}}}
	backends, err := NewResolver(fake, 40).Backends(ctx, "did:plc:caller", "did:plc:org")
	require.NoError(t, err)
	require.Len(t, backends, 1)
	b := backends[0]
	require.Equal(t, BackendID, b.ID())
	require.Equal(t, BackendKind, b.Kind())

	results, err := b.Search(ctx, fedsearch.Query{Text: "hi", Limit: 24})
	require.NoError(t, err)
	require.Equal(t, syntax.DID("did:plc:caller"), fake.caller)
	require.Equal(
		t,
		Query{Text: "hi", Org: "did:plc:org", Limit: 24, SnippetLength: 40},
		fake.query,
	)
	require.Len(t, results, 1)
	require.Equal(t, uri.String(), results[0].ID)
	require.Equal(t, "say hi", results[0].Snippet)
	require.Equal(t, ResultKind, results[0].Kind)
	var data RecordData
	require.NoError(t, json.Unmarshal(results[0].Data, &data))
	require.Equal(t, RecordData{
		Space: space, Collection: "com.example.note", Value: map[string]any{"text": "hi"},
	}, data)

	fake.err = errors.New("meilisearch down")
	_, err = b.Search(ctx, fedsearch.Query{Text: "hi"})
	require.Error(t, err)
}
