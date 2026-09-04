package pearserver_test

import (
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestServer_CreateOpensocialSpace(t *testing.T) {
	t.Run("requires membership", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		client := httpx_testutil.NewTestXRPCClient(t)
		var out opensocial_api.CommunityOpensocialCreateSpaceOutput
		code := client.Procedure(
			ts.Server.CreateOpensocialSpace,
			opensocial_api.CommunityOpensocialCreateSpaceInput{
				Org:  orgDID,
				Type: "network.habitat.docs",
			},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("creates a space with the given roles", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		client := httpx_testutil.NewTestXRPCClient(t)
		var out opensocial_api.CommunityOpensocialCreateSpaceOutput
		code := client.Procedure(
			ts.Server.CreateOpensocialSpace,
			opensocial_api.CommunityOpensocialCreateSpaceInput{
				Org:   orgDID,
				Type:  "network.habitat.docs",
				Roles: []string{"admin", "member"},
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
		require.NotEmpty(t, out.Uri)

		spaceURI, err := habitat_syntax.ParseSpaceURI(out.Uri)
		require.NoError(t, err)

		allowed, err := ts.OpenSocialStore.CheckPermission(t.Context(), alice, spaceURI)
		require.NoError(t, err)
		require.False(t, allowed, "alice holds no role in this org yet")

		require.NoError(t, ts.OpenSocialStore.AssignRoles(
			t.Context(), syntax.DID(orgDID), alice, []string{"member"},
		))
		allowed, err = ts.OpenSocialStore.CheckPermission(t.Context(), alice, spaceURI)
		require.NoError(t, err)
		require.True(t, allowed, "member role should be able to read the new space")
	})

	t.Run("rejects a duplicate skey", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		client := httpx_testutil.NewTestXRPCClient(t)
		input := opensocial_api.CommunityOpensocialCreateSpaceInput{
			Org:  orgDID,
			Type: "network.habitat.docs",
			Skey: "fixed",
		}
		var out opensocial_api.CommunityOpensocialCreateSpaceOutput
		code := client.Procedure(ts.Server.CreateOpensocialSpace, input, &out)
		require.Equal(t, http.StatusOK, code)

		code = client.Procedure(ts.Server.CreateOpensocialSpace, input, &out)
		require.Equal(t, http.StatusBadRequest, code)
	})
}
