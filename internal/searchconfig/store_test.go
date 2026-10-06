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

	require.NoError(t, s.Add(t.Context(), orgA, "com.example.post"))
	require.NoError(t, s.Add(t.Context(), orgA, "com.example.note"))
	// Adding again does nothing.
	require.NoError(t, s.Add(t.Context(), orgA, "com.example.post"))
	require.NoError(t, s.Add(t.Context(), orgB, "com.example.other"))

	got, err = s.List(t.Context(), orgA)
	require.NoError(t, err)
	require.Equal(t, []syntax.NSID{"com.example.note", "com.example.post"}, got)
	got, err = s.Collections(t.Context(), orgB)
	require.NoError(t, err)
	require.Equal(t, []syntax.NSID{"com.example.other"}, got)

	require.NoError(t, s.Remove(t.Context(), orgA, "com.example.post"))
	// Removing one that isn't configured does nothing.
	require.NoError(t, s.Remove(t.Context(), orgA, "com.example.post"))
	got, err = s.List(t.Context(), orgA)
	require.NoError(t, err)
	require.Equal(t, []syntax.NSID{"com.example.note"}, got)

	require.Equal(t, searchconfig.DefaultCollections, s.Defaults())
}
