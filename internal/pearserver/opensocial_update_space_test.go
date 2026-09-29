package pearserver_test

import (
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestServer_UpdateOpensocialSpace(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("requires membership", func(t *testing.T) {
		adminTS, aliceTS, _ := newSharedOpenSocialServers(t)
		orgDID, err := adminTS.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		spaceURI, err := adminTS.OpenSocialStore.CreateSpace(
			t.Context(), syntax.DID(orgDID), []string{"admin", "member"},
			"network.habitat.docs", "shared",
		)
		require.NoError(t, err)

		var out struct{}
		code := client.Procedure(
			aliceTS.Server.UpdateOpensocialSpace,
			opensocial_api.CommunityOpensocialUpdateSpaceInput{
				Space: spaceURI.String(),
				Roles: []string{"admin"},
			},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("updates the roles granted access to a space", func(t *testing.T) {
		adminTS, _, _ := newSharedOpenSocialServers(t)
		orgDID, err := adminTS.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		org := syntax.DID(orgDID)

		spaceURI, err := adminTS.OpenSocialStore.CreateSpace(
			t.Context(), org, []string{"admin", "member"},
			"network.habitat.docs", "shared",
		)
		require.NoError(t, err)

		// alice is a member, so she can read the space before the update.
		require.NoError(t, adminTS.OpenSocialStore.AssignRoles(
			t.Context(), org, alice, []string{opensocial.MemberRoleRkey},
		))
		allowed, err := adminTS.OpenSocialStore.CheckPermission(t.Context(), alice, spaceURI)
		require.NoError(t, err)
		require.True(t, allowed)

		var out struct{}
		code := client.Procedure(
			adminTS.Server.UpdateOpensocialSpace,
			opensocial_api.CommunityOpensocialUpdateSpaceInput{
				Space: spaceURI.String(),
				Roles: []string{"admin"},
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)

		// The access record now grants only the admin role.
		allowed, err = adminTS.OpenSocialStore.CheckPermission(t.Context(), alice, spaceURI)
		require.NoError(t, err)
		require.False(t, allowed)
		allowed, err = adminTS.OpenSocialStore.CheckPermission(t.Context(), admin, spaceURI)
		require.NoError(t, err)
		require.True(t, allowed)
	})

	t.Run("rejects a space that does not exist", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		org := syntax.DID(orgDID)

		var out struct{}
		code := client.Procedure(
			ts.Server.UpdateOpensocialSpace,
			opensocial_api.CommunityOpensocialUpdateSpaceInput{
				Space: habitat_syntax.ConstructSpaceURI(
					org, "community.opensocial.channel", "nonexistent",
				).String(),
				Roles: []string{"admin"},
			},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})
}
