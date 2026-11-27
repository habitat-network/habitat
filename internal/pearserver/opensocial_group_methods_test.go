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

func TestServer_GroupSpaceMethods(t *testing.T) {
	adminTS, aliceTS, _ := newSharedOpenSocialServers(t)
	org, err := adminTS.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
	require.NoError(t, err)
	client := httpx_testutil.NewTestXRPCClient(t)

	// alice is a plain member: she can participate but not create spaces.
	require.NoError(t, adminTS.OpenSocialStore.AssignRoles(
		t.Context(), syntax.DID(org), alice, []string{opensocial.MemberRoleRkey},
	))

	var created opensocial_api.GroupOpensocialCreateSpaceOutput
	access := opensocial_api.GroupOpensocialDefsAccessConfig{
		Grants: []opensocial_api.GroupOpensocialDefsSpaceGrant{
			{Role: opensocial.MemberRoleRkey, Scopes: []string{"read"}},
		},
	}
	body := func(in opensocial_api.GroupOpensocialCreateSpaceInput) map[string]any {
		return map[string]any{
			"group": org, "spaceType": in.SpaceType, "access": in.Access, "skey": in.Skey,
		}
	}

	t.Run("a member without manage=create cannot create a space", func(t *testing.T) {
		code := client.Procedure(aliceTS.Server.GroupCreateSpace, body(
			opensocial_api.GroupOpensocialCreateSpaceInput{
				SpaceType: "network.habitat.docs", Access: access,
			}), &created)
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("an admin creates a space with its access record and an index entry", func(t *testing.T) {
		code := client.Procedure(adminTS.Server.GroupCreateSpace, body(
			opensocial_api.GroupOpensocialCreateSpaceInput{
				SpaceType: "network.habitat.docs", Access: access,
			}), &created)
		require.Equal(t, http.StatusOK, code)
		require.NotEmpty(t, created.Space)
		require.NotEmpty(t, created.Access.Uri)
		require.NotNil(t, created.Index)
		spaceURI, err := habitat_syntax.ParseSpaceURI(created.Space)
		require.NoError(t, err)
		ok, err := adminTS.OpenSocialStore.CheckPermission(t.Context(), alice, spaceURI)
		require.NoError(t, err)
		require.True(t, ok, "the member role was granted read")
	})

	t.Run("reserved spaces, unknown roles and bad scopes are refused", func(t *testing.T) {
		for name, in := range map[string]opensocial_api.GroupOpensocialCreateSpaceInput{
			"reserved": {SpaceType: opensocial.MetaSpaceType, Access: access},
			"unknown role": {SpaceType: "network.habitat.docs", Skey: "u", Access: opensocial_api.GroupOpensocialDefsAccessConfig{
				Grants: []opensocial_api.GroupOpensocialDefsSpaceGrant{{Role: "ghost", Scopes: []string{"read"}}},
			}},
			"bad scope": {SpaceType: "network.habitat.docs", Skey: "b", Access: opensocial_api.GroupOpensocialDefsAccessConfig{
				Grants: []opensocial_api.GroupOpensocialDefsSpaceGrant{{Role: "member", Scopes: []string{"nonsense"}}},
			}},
		} {
			code := client.Procedure(adminTS.Server.GroupCreateSpace, body(in), &created)
			require.Equalf(t, http.StatusBadRequest, code, name)
		}
	})

	t.Run("deleteSpace removes the space but never meta or members", func(t *testing.T) {
		var first opensocial_api.GroupOpensocialCreateSpaceOutput
		require.Equal(t, http.StatusOK, client.Procedure(adminTS.Server.GroupCreateSpace, body(
			opensocial_api.GroupOpensocialCreateSpaceInput{
				SpaceType: "network.habitat.docs", Skey: "gone", Access: access,
			}), &first))
		require.Equal(t, http.StatusUnauthorized, client.Procedure(aliceTS.Server.GroupDeleteSpace,
			opensocial_api.GroupOpensocialDeleteSpaceInput{Space: first.Space}, &struct{}{}))
		require.Equal(t, http.StatusOK, client.Procedure(adminTS.Server.GroupDeleteSpace,
			opensocial_api.GroupOpensocialDeleteSpaceInput{Space: first.Space}, &struct{}{}))
		require.Equal(t, http.StatusBadRequest, client.Procedure(adminTS.Server.GroupDeleteSpace,
			opensocial_api.GroupOpensocialDeleteSpaceInput{
				Space: habitat_syntax.ConstructSpaceURI(
					syntax.DID(org), opensocial.MetaSpaceType, "self",
				).AtSpace().String(),
			}, &struct{}{}))
	})

	t.Run("leaveGroup removes the caller's membership", func(t *testing.T) {
		require.Equal(t, http.StatusOK, client.Procedure(aliceTS.Server.GroupLeaveGroup,
			opensocial_api.GroupOpensocialLeaveGroupInput{Group: org}, &struct{}{}))
		roles, err := adminTS.OpenSocialStore.GetUserRoles(t.Context(), syntax.DID(org), alice)
		require.NoError(t, err)
		require.Empty(t, roles)
	})

	t.Run("requestJoin follows the join policy", func(t *testing.T) {
		var out opensocial_api.GroupOpensocialRequestJoinOutput
		in := opensocial_api.GroupOpensocialRequestJoinInput{Group: org}
		require.Equal(t, http.StatusForbidden,
			client.Procedure(aliceTS.Server.GroupRequestJoin, in, &out), "invite-only refuses")

		require.NoError(t, adminTS.OpenSocialStore.UpdateProfile(
			t.Context(), syntax.DID(org), "acme", "", "open",
		))
		require.Equal(t, http.StatusOK, client.Procedure(aliceTS.Server.GroupRequestJoin, in, &out))
		require.Equal(t, "admitted", out.Status)
		roles, err := adminTS.OpenSocialStore.GetUserRoles(t.Context(), syntax.DID(org), alice)
		require.NoError(t, err)
		require.Equal(t, []string{opensocial.MemberRoleRkey}, roles, "the default role")
	})
}
