package pearserver_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestServer_NotifyCredentialRevoked(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	// The default test validator authenticates service auth as did:plc:owner.
	ownedSpace := habitat_syntax.ConstructSpaceURI("did:plc:owner", groupTp, "s1")
	foreignSpace := habitat_syntax.ConstructSpaceURI("did:plc:org", groupTp, "s1")

	revoke := func(space habitat_syntax.SpaceURI, jtis ...string) int {
		var out struct{}
		return httpx_testutil.NewTestXRPCClient(t).Procedure(
			ts.Server.NotifyCredentialRevoked,
			habitat.NetworkHabitatSpaceNotifyCredentialRevokedInput{
				Space: space.String(), Jtis: jtis,
			},
			&out,
		)
	}

	t.Run("space authority revokes credentials idempotently", func(t *testing.T) {
		require.Equal(t, http.StatusOK, revoke(ownedSpace, "jti-1", "jti-2"))
		require.Equal(t, http.StatusOK, revoke(ownedSpace, "jti-1"))

		for _, jti := range []string{"jti-1", "jti-2"} {
			revoked, err := ts.Revocations.IsRevoked(t.Context(), ownedSpace, jti)
			require.NoError(t, err)
			require.True(t, revoked, jti)
		}
		revoked, err := ts.Revocations.IsRevoked(t.Context(), ownedSpace, "other")
		require.NoError(t, err)
		require.False(t, revoked)
	})

	t.Run("only the space authority can revoke", func(t *testing.T) {
		require.Equal(t, http.StatusUnauthorized, revoke(foreignSpace, "jti-3"))
		revoked, err := ts.Revocations.IsRevoked(t.Context(), foreignSpace, "jti-3")
		require.NoError(t, err)
		require.False(t, revoked)
	})

	t.Run("requires at least one jti", func(t *testing.T) {
		require.Equal(t, http.StatusBadRequest, revoke(ownedSpace))
	})
}
