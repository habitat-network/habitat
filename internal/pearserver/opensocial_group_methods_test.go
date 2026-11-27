package pearserver_test

import (
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/comatproto"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestServer_GroupSpaceMethods(t *testing.T) {
	adminTS, aliceTS, _ := newSharedOpenSocialServers(t)
	org, err := adminTS.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
	require.NoError(t, err)
	client := httpx_testutil.NewTestXRPCClient(t)
	// Until group OAuth sessions carry the authorizing user, a session for the
	// group DID is the group acting as itself.
	groupTS := newOpenSocialServerAs(
		t, adminTS, syntax.DID(org), authn.ValidatorMethodServiceAuth,
	)

	require.NoError(t, adminTS.OpenSocialStore.AssignRoles(
		t.Context(), syntax.DID(org), alice, []string{opensocial.MemberRoleRkey},
	))

	var created comatproto.ComAtprotoSpaceCreateSpaceOutput
	policy := func(grants ...map[string]any) map[string]any {
		return map[string]any{"$type": spaces.PolicyGroupSpace, "grants": grants}
	}
	memberRead := map[string]any{"role": opensocial.MemberRoleRkey, "read": true}
	create := func(skey string, p map[string]any) comatproto.ComAtprotoSpaceCreateSpaceInput {
		return comatproto.ComAtprotoSpaceCreateSpaceInput{
			SpaceType: "network.habitat.docs", Skey: skey, Policy: p,
		}
	}

	t.Run("a user cannot use the group policy", func(t *testing.T) {
		code := client.Procedure(
			aliceTS.Server.CreateSpace,
			create("u", policy(memberRead)),
			&created,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("a group creates a space whose policy grants a role read", func(t *testing.T) {
		code := client.Procedure(
			groupTS.Server.CreateSpace,
			create("", policy(memberRead)),
			&created,
		)
		require.Equal(t, http.StatusOK, code)
		require.NotEmpty(t, created.Space)
		spaceURI, err := habitat_syntax.ParseSpaceURI(created.Space)
		require.NoError(t, err)
		ok, err := adminTS.OpenSocialStore.CheckPermission(t.Context(), alice, spaceURI)
		require.NoError(t, err)
		require.True(t, ok, "the member role was granted read")
	})

	t.Run("reserved spaces, unknown roles and bad permissions are refused", func(t *testing.T) {
		for name, in := range map[string]comatproto.ComAtprotoSpaceCreateSpaceInput{
			"reserved":     {SpaceType: opensocial.MetaSpaceType, Policy: policy(memberRead)},
			"unknown role": create("u1", policy(map[string]any{"role": "ghost", "read": true})),
			"bad permission": create("u2", policy(map[string]any{
				"role": opensocial.MemberRoleRkey, "permissions": []string{"nonsense"},
			})),
		} {
			code := client.Procedure(groupTS.Server.CreateSpace, in, &created)
			require.Equalf(t, http.StatusBadRequest, code, name)
		}
	})

	t.Run("deleteSpace removes the space but never meta or members", func(t *testing.T) {
		var first comatproto.ComAtprotoSpaceCreateSpaceOutput
		require.Equal(t, http.StatusOK, client.Procedure(
			groupTS.Server.CreateSpace, create("gone", policy(memberRead)), &first))
		require.Equal(t, http.StatusOK, client.Procedure(groupTS.Server.DeleteSpace,
			comatproto.ComAtprotoSpaceDeleteSpaceInput{Space: first.Space}, &struct{}{}))
		require.Equal(t, http.StatusBadRequest, client.Procedure(groupTS.Server.DeleteSpace,
			comatproto.ComAtprotoSpaceDeleteSpaceInput{
				Space: habitat_syntax.ConstructSpaceURI(
					syntax.DID(org), opensocial.MetaSpaceType, "self",
				).AtSpace().String(),
			}, &struct{}{}))
	})

	t.Run("leaveGroup removes the caller's membership", func(t *testing.T) {
		require.Equal(t, http.StatusOK, client.Procedure(aliceTS.Server.GroupLeaveGroup,
			opensocial_api.OrgAtgrpLeaveGroupInput{Group: org}, &struct{}{}))
		roles, err := adminTS.OpenSocialStore.GetUserRoles(t.Context(), syntax.DID(org), alice)
		require.NoError(t, err)
		require.Empty(t, roles)
	})

	t.Run("requestJoin follows the join policy", func(t *testing.T) {
		var out opensocial_api.OrgAtgrpRequestJoinOutput
		in := opensocial_api.OrgAtgrpRequestJoinInput{Group: org}
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
