package spacestream

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestParseQuery(t *testing.T) {
	tests := []struct {
		name    string
		query   url.Values
		want    Query
		wantErr bool
	}{
		{name: "empty matches all from the live tip", query: url.Values{}},
		{
			name:  "cursor",
			query: url.Values{"cursor": {"42"}},
			want:  Query{Cursor: new(spaces.EventSeq(42))},
		},
		{
			name:  "zero cursor replays everything",
			query: url.Values{"cursor": {"0"}},
			want:  Query{Cursor: new(spaces.EventSeq(0))},
		},
		{name: "negative cursor", query: url.Values{"cursor": {"-1"}}, wantErr: true},
		{name: "malformed cursor", query: url.Values{"cursor": {"x"}}, wantErr: true},
		{
			name: "filters, deduplicated",
			query: url.Values{
				"spaces":      {string(spaceA), string(spaceB), string(spaceA)},
				"dids":        {"did:plc:alice", "did:plc:alice"},
				"collections": {"network.habitat.note", "app.bsky.*", "app.bsky.*"},
			},
			want: Query{Filter: spaces.EventFilter{
				Spaces:             []habitat_syntax.SpaceURI{spaceA, spaceB},
				Repos:              []syntax.DID{alice},
				Collections:        []syntax.NSID{"network.habitat.note"},
				CollectionPrefixes: []string{"app.bsky."},
			}},
		},
		{name: "invalid space", query: url.Values{"spaces": {"at://nope"}}, wantErr: true},
		{name: "invalid did", query: url.Values{"dids": {"alice"}}, wantErr: true},
		{name: "invalid nsid", query: url.Values{"collections": {"note"}}, wantErr: true},
		{name: "bare wildcard", query: url.Values{"collections": {"*"}}, wantErr: true},
		{
			name:    "wildcard mid-pattern",
			query:   url.Values{"collections": {"app.bsky.fo*"}},
			wantErr: true,
		},
		{
			name:    "too many collections",
			query:   url.Values{"collections": manyCollections(MaxCollections + 1)},
			wantErr: true,
		},
		{
			name:  "collections at the cap",
			query: url.Values{"collections": manyCollections(MaxCollections)},
			want: Query{Filter: spaces.EventFilter{
				Collections: nsids(manyCollections(MaxCollections)),
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseQuery(tt.query)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidQuery)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func manyCollections(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("network.habitat.c%d", i)
	}
	return out
}

func nsids(values []string) []syntax.NSID {
	out := make([]syntax.NSID, len(values))
	for i, v := range values {
		out[i] = syntax.NSID(v)
	}
	return out
}
