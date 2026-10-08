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

func TestFilterAlsoOrgs(t *testing.T) {
	require.Equal(t,
		[][]string{{
			`(space_owner = "did:plc:org" AND collection IN ["com.example.a"])`,
			`(space_owner = "did:web:everyone" AND collection IN ["com.example.b"])`,
		}},
		filter(Query{
			Org:         "did:plc:org",
			Collections: []syntax.NSID{"com.example.a"},
			Also: []Scope{{
				Org: "did:web:everyone", Collections: []syntax.NSID{"com.example.b"},
			}},
		}))
}
