package searchconfig_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/searchconfig"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
)

func TestStore(t *testing.T) {
	spaces := spaces_testutil.NewTestStore(
		t, spaces_testutil.WithDB(pear_testutil.NewPearDB(t)),
	)
	s := searchconfig.NewStore(spaces)
	orgA := syntax.DID("did:web:a.example.com")
	orgB := syntax.DID("did:web:b.example.com")
	for _, org := range []syntax.DID{orgA, orgB} {
		_, err := spaces.CreateSpace(t.Context(), org, opensocial.MembersSpaceType, "self")
		require.NoError(t, err)
	}

	got, err := s.List(t.Context(), orgA)
	require.NoError(t, err)
	require.Empty(t, got)

	require.NoError(t, s.Put(t.Context(), orgA, searchconfig.Config{
		Collection:       "com.example.post",
		CrawlableFields:  []string{"text"},
		FilterableFields: []string{"author"},
	}))
	require.NoError(
		t,
		s.Put(t.Context(), orgA, searchconfig.Config{Collection: "com.example.note"}),
	)
	require.NoError(
		t,
		s.Put(t.Context(), orgB, searchconfig.Config{Collection: "com.example.other"}),
	)
	// Putting again replaces the config.
	require.NoError(t, s.Put(t.Context(), orgA, searchconfig.Config{
		Collection:      "com.example.post",
		CrawlableFields: []string{"body"},
	}))

	got, err = s.List(t.Context(), orgA)
	require.NoError(t, err)
	require.Equal(t, []searchconfig.Config{
		{Collection: "com.example.note"},
		{Collection: "com.example.post", CrawlableFields: []string{"body"}},
	}, got)

	byOrg, err := s.Collections(t.Context(), orgA, orgB, "did:web:none.example.com")
	require.NoError(t, err)
	require.Equal(t, map[syntax.DID][]syntax.NSID{
		orgA: {"com.example.note", "com.example.post"},
		orgB: {"com.example.other"},
	}, byOrg)

	require.NoError(t, s.Remove(t.Context(), orgA, "com.example.post"))
	// Removing one that isn't configured does nothing.
	require.NoError(t, s.Remove(t.Context(), orgA, "com.example.post"))
	got, err = s.List(t.Context(), orgA)
	require.NoError(t, err)
	require.Equal(t, []searchconfig.Config{{Collection: "com.example.note"}}, got)

	require.Equal(t, searchconfig.DefaultCollections, s.Defaults())
}
