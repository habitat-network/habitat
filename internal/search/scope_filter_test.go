package search

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

func TestScopeFilter(t *testing.T) {
	t.Run("defaults and owners are ORed", func(t *testing.T) {
		got := filter(Query{Scope: &CollectionScope{
			Default: []syntax.NSID{"com.example.d"},
			ByOwner: map[syntax.DID][]syntax.NSID{
				"did:plc:b": {"com.example.y"},
				"did:plc:a": {"com.example.x", "com.example.z"},
			},
		}})
		require.Equal(t, [][]string{{
			`collection IN ["com.example.d"]`,
			`(space_owner = "did:plc:a" AND collection IN ["com.example.x", "com.example.z"])`,
			`(space_owner = "did:plc:b" AND collection IN ["com.example.y"])`,
		}}, got)
	})

	t.Run("nothing allowed matches nothing", func(t *testing.T) {
		require.Equal(t,
			[][]string{{`collection = ""`}},
			filter(Query{Scope: &CollectionScope{}}))
	})

	t.Run("nil scope allows everything", func(t *testing.T) {
		require.Empty(t, filter(Query{}))
	})
}
