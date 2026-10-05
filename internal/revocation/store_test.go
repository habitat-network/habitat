package revocation_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"
	"github.com/habitat-network/habitat/internal/revocation"
)

func TestStore(t *testing.T) {
	s := revocation.NewStore(pear_testutil.NewPearDB(t))
	const space = "at://did:plc:owner/space/com.test.space/abc"

	revoked, err := s.IsRevoked(t.Context(), space, "a")
	require.NoError(t, err)
	require.False(t, revoked)

	require.NoError(t, s.Revoke(t.Context(), space, []string{"a", "b"}))
	require.NoError(t, s.Revoke(t.Context(), space, []string{"a"})) // idempotent

	revoked, err = s.IsRevoked(t.Context(), space, "a")
	require.NoError(t, err)
	require.True(t, revoked)

	// Revocation is scoped to the space it was issued for.
	revoked, err = s.IsRevoked(t.Context(), "at://did:plc:owner/space/com.test.space/other", "a")
	require.NoError(t, err)
	require.False(t, revoked)
}
