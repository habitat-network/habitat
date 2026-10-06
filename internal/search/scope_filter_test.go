package search

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

func TestFilterOrg(t *testing.T) {
	require.Equal(t,
		[][]string{{`collection IN ["com.example.a"]`}, {`space_owner = "did:plc:org"`}},
		filter(Query{Org: "did:plc:org", Collections: []syntax.NSID{"com.example.a"}}))
	require.Empty(t, filter(Query{}))
}
