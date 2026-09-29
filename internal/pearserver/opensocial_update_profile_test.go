package pearserver_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
)

func TestServer_UpdateProfile(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("requires admin", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var out struct{}
		code := client.Procedure(
			ts.Server.UpdateProfile,
			opensocial_api.CommunityOpensocialUpdateProfileInput{
				Org: orgDID, Name: "Acme Corp",
			},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("updates profile", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var out struct{}
		code := client.Procedure(
			ts.Server.UpdateProfile,
			opensocial_api.CommunityOpensocialUpdateProfileInput{
				Org: orgDID, Name: "Acme Corp", Description: "We make widgets",
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
	})
}
