package pearserver_test

import (
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
)

func TestServer_RevokeAppAccess(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)
	const clientID = "https://app.example.com/client-metadata.json"

	t.Run("requires community.configure", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		require.NoError(
			t,
			ts.OpenSocialStore.GrantAppAccess(t.Context(), syntax.DID(orgDID), clientID, nil),
		)

		var out struct{}
		code := client.Procedure(
			ts.Server.RevokeAppAccess,
			habitat.NetworkHabitatOpensocialRevokeAppAccessInput{Org: orgDID, ClientId: clientID},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)

		ok, err := ts.OpenSocialStore.HasAppAccess(t.Context(), syntax.DID(orgDID), clientID)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("unknown app", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var out struct{}
		code := client.Procedure(
			ts.Server.RevokeAppAccess,
			habitat.NetworkHabitatOpensocialRevokeAppAccessInput{Org: orgDID, ClientId: clientID},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("admin revokes an app", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		require.NoError(
			t,
			ts.OpenSocialStore.GrantAppAccess(t.Context(), syntax.DID(orgDID), clientID, nil),
		)

		var out struct{}
		code := client.Procedure(
			ts.Server.RevokeAppAccess,
			habitat.NetworkHabitatOpensocialRevokeAppAccessInput{Org: orgDID, ClientId: clientID},
			&out,
		)
		require.Equal(t, http.StatusOK, code)

		ok, err := ts.OpenSocialStore.HasAppAccess(t.Context(), syntax.DID(orgDID), clientID)
		require.NoError(t, err)
		require.False(t, ok)
	})
}
