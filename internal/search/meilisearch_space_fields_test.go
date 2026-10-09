package search

import (
	"os"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/meilisearch/meilisearch-go"
	"github.com/stretchr/testify/require"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// TestMeilisearch_SpaceURIFields checks that a document's space URI is also
// indexed as separate owner, type and key fields. It runs against the
// Meilisearch at HABITAT_TEST_MEILISEARCH_URL, which the external tests also
// use.
func TestMeilisearch_SpaceURIFields(t *testing.T) {
	url := os.Getenv("HABITAT_TEST_MEILISEARCH_URL")
	if url == "" {
		t.Skip("HABITAT_TEST_MEILISEARCH_URL is not set")
	}
	uid := "test_space_fields_" + syntax.NewTIDNow(0).String()
	idx, err := NewMeilisearch(t.Context(), url, "", uid)
	require.NoError(t, err)

	space := habitat_syntax.ConstructSpaceURI("did:plc:owner", "com.example.type", "key1")
	uri := habitat_syntax.ConstructSpaceRecordURI(space, "did:plc:repo", "com.example.note", "r1")
	require.NoError(t, idx.Put(t.Context(), Document{
		URI:        uri,
		Space:      space,
		Repo:       "did:plc:repo",
		Collection: "com.example.note",
		Rev:        syntax.NewTIDNow(0),
		Text:       "hello",
		Access:     Access{Public: true},
	}))

	for _, filter := range []string{
		`space_owner = "did:plc:owner"`,
		`space_type = "com.example.type"`,
		`space_key = "key1"`,
	} {
		resp, err := idx.index.SearchWithContext(t.Context(), "", &meilisearch.SearchRequest{
			Filter: filter,
		})
		require.NoError(t, err, filter)
		require.Len(t, resp.Hits, 1, filter)
	}
}
