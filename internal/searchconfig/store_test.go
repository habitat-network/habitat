package searchconfig_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"
	"github.com/habitat-network/habitat/internal/searchconfig"
)

func TestStore(t *testing.T) {
	s := searchconfig.NewStore(pear_testutil.NewPearDB(t))
	orgA := syntax.DID("did:web:a.example.com")
	orgB := syntax.DID("did:web:b.example.com")

	got, err := s.List(t.Context(), orgA)
	require.NoError(t, err)
	require.Empty(t, got)

	require.NoError(t, s.Add(t.Context(), orgA, "com.example.post"))
	require.NoError(t, s.Add(t.Context(), orgA, "com.example.note"))
	// Adding again is a no-op.
	require.NoError(t, s.Add(t.Context(), orgA, "com.example.post"))
	require.NoError(t, s.Add(t.Context(), orgB, "com.example.other"))

	got, err = s.List(t.Context(), orgA)
	require.NoError(t, err)
	require.Equal(t, []syntax.NSID{"com.example.note", "com.example.post"}, got)

	byOrg, err := s.ListAll(t.Context())
	require.NoError(t, err)
	require.Len(t, byOrg, 2)
	require.Equal(t, []syntax.NSID{"com.example.other"}, byOrg[orgB])

	require.NoError(t, s.Remove(t.Context(), orgA, "com.example.post"))
	// Removing one that isn't configured is a no-op.
	require.NoError(t, s.Remove(t.Context(), orgA, "com.example.post"))
	got, err = s.List(t.Context(), orgA)
	require.NoError(t, err)
	require.Equal(t, []syntax.NSID{"com.example.note"}, got)

	// Defaults are fixed and not part of the configured list.
	require.Equal(t, searchconfig.DefaultCollections, s.Defaults())
}
