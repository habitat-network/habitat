package pearserver_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
)

func TestServer_JoinRequests(t *testing.T) {
	adminTS, aliceTS, _ := newSharedOpenSocialServers(t)
	org, err := adminTS.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
	require.NoError(t, err)
	group := syntax.DID(org)
	require.NoError(t, adminTS.OpenSocialStore.UpdateProfile(
		t.Context(), group, "acme", "", opensocial.JoinPolicyApproval,
	))
	groupTS := newOpenSocialServerAs(t, adminTS, group, authn.ValidatorMethodOAuth)
	client := httpx_testutil.NewTestXRPCClient(t)

	list := func() opensocial_api.OrgAtgrpListJoinRequestsOutput {
		var out opensocial_api.OrgAtgrpListJoinRequestsOutput
		require.Equal(
			t,
			http.StatusOK,
			client.Query(groupTS.Server.GroupListJoinRequests, nil, &out),
		)
		return out
	}

	t.Run(
		"an approval group queues a request and returns it again when asked twice",
		func(t *testing.T) {
			in := opensocial_api.OrgAtgrpRequestJoinInput{Group: org, Message: "let me in"}
			for range 2 {
				var out opensocial_api.OrgAtgrpRequestJoinOutput
				require.Equal(
					t,
					http.StatusOK,
					client.Procedure(aliceTS.Server.GroupRequestJoin, in, &out),
				)
				require.Equal(t, "pending", out.Status)
				require.Equal(t, alice.String(), out.Request.Member)
				require.Equal(t, "let me in", out.Request.Message)
			}
			requests := list().Requests
			require.Len(t, requests, 1)
			require.Equal(t, alice.String(), requests[0].Member)
		},
	)

	t.Run("only the group may list or deny requests", func(t *testing.T) {
		var out opensocial_api.OrgAtgrpListJoinRequestsOutput
		require.Equal(t, http.StatusUnauthorized,
			client.Query(aliceTS.Server.GroupListJoinRequests, nil, &out))
		require.Equal(t, http.StatusUnauthorized, client.Procedure(
			aliceTS.Server.GroupDenyJoinRequest,
			opensocial_api.OrgAtgrpDenyJoinRequestInput{Member: alice.String()}, &struct{}{}))
	})

	t.Run("writing the membership approves the request", func(t *testing.T) {
		require.NoError(t, adminTS.OpenSocialStore.AssignRoles(
			t.Context(), group, alice, []string{opensocial.MemberRoleRkey},
		))
		require.Empty(t, list().Requests)
	})

	t.Run("a requester can cancel and the group can deny", func(t *testing.T) {
		require.NoError(t, adminTS.OpenSocialStore.EjectMember(t.Context(), group, alice))
		var out opensocial_api.OrgAtgrpRequestJoinOutput
		in := opensocial_api.OrgAtgrpRequestJoinInput{Group: org}
		require.Equal(t, http.StatusOK, client.Procedure(aliceTS.Server.GroupRequestJoin, in, &out))

		cancel := opensocial_api.OrgAtgrpCancelJoinRequestInput{Group: org}
		require.Equal(t, http.StatusOK,
			client.Procedure(aliceTS.Server.GroupCancelJoinRequest, cancel, &struct{}{}))
		require.Equal(
			t,
			http.StatusBadRequest,
			client.Procedure(
				aliceTS.Server.GroupCancelJoinRequest,
				cancel,
				&struct{}{},
			),
			"already gone",
		)

		require.Equal(t, http.StatusOK, client.Procedure(aliceTS.Server.GroupRequestJoin, in, &out))
		deny := opensocial_api.OrgAtgrpDenyJoinRequestInput{Member: alice.String()}
		require.Equal(t, http.StatusOK,
			client.Procedure(groupTS.Server.GroupDenyJoinRequest, deny, &struct{}{}))
		require.Empty(t, list().Requests)
		require.Equal(t, http.StatusBadRequest,
			client.Procedure(groupTS.Server.GroupDenyJoinRequest, deny, &struct{}{}))
	})

	t.Run("an invite-only group refuses a request without an invite", func(t *testing.T) {
		require.NoError(t, adminTS.OpenSocialStore.UpdateProfile(
			t.Context(), group, "acme", "", opensocial.JoinPolicyInvite,
		))
		var out opensocial_api.OrgAtgrpRequestJoinOutput
		in := opensocial_api.OrgAtgrpRequestJoinInput{Group: org}
		require.Equal(
			t,
			http.StatusForbidden,
			client.Procedure(aliceTS.Server.GroupRequestJoin, in, &out),
		)
	})
}

func TestServer_JoinRequestsPagination(t *testing.T) {
	adminTS, _, _ := newSharedOpenSocialServers(t)
	org, err := adminTS.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
	require.NoError(t, err)
	group := syntax.DID(org)
	groupTS := newOpenSocialServerAs(t, adminTS, group, authn.ValidatorMethodOAuth)
	client := httpx_testutil.NewTestXRPCClient(t)
	for _, member := range []syntax.DID{"did:plc:a", "did:plc:b", "did:plc:c"} {
		_, err := adminTS.OpenSocialStore.QueueJoinRequest(t.Context(), group, member, "")
		require.NoError(t, err)
	}

	var seen []string
	cursor := ""
	for range 3 {
		var out opensocial_api.OrgAtgrpListJoinRequestsOutput
		require.Equal(t, http.StatusOK, client.Query(
			groupTS.Server.GroupListJoinRequests,
			url.Values{"limit": {"2"}, "cursor": {cursor}}, &out))
		for _, request := range out.Requests {
			seen = append(seen, request.Member)
		}
		if out.Cursor == "" {
			break
		}
		cursor = out.Cursor
	}
	require.ElementsMatch(t, []string{"did:plc:a", "did:plc:b", "did:plc:c"}, seen)

	var out opensocial_api.OrgAtgrpListJoinRequestsOutput
	require.Equal(t, http.StatusBadRequest, client.Query(
		groupTS.Server.GroupListJoinRequests, url.Values{"cursor": {"junk"}}, &out))
	require.Equal(t, http.StatusBadRequest, client.Query(
		groupTS.Server.GroupListJoinRequests, url.Values{"limit": {"500"}}, &out))
}

func TestServer_RequestJoinByPolicy(t *testing.T) {
	adminTS, aliceTS, _ := newSharedOpenSocialServers(t)
	client := httpx_testutil.NewTestXRPCClient(t)
	newGroup := func(t *testing.T, handle, policy string) string {
		t.Helper()
		org, err := adminTS.OpenSocialStore.NewOrg(t.Context(), handle, admin)
		require.NoError(t, err)
		require.NoError(t, adminTS.OpenSocialStore.UpdateProfile(
			t.Context(), syntax.DID(org), handle, "", policy,
		))
		return org
	}
	join := func(org string, invite string) (int, opensocial_api.OrgAtgrpRequestJoinOutput) {
		var out opensocial_api.OrgAtgrpRequestJoinOutput
		code := client.Procedure(aliceTS.Server.GroupRequestJoin,
			opensocial_api.OrgAtgrpRequestJoinInput{Group: org, Invite: invite}, &out)
		return code, out
	}

	t.Run("an open group admits anyone, and asking again keeps them in", func(t *testing.T) {
		org := newGroup(t, "open-group", opensocial.JoinPolicyOpen)
		code, out := join(org, "")
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "admitted", out.Status)
		require.NotNil(t, out.Membership)
		roles, err := adminTS.OpenSocialStore.GetUserRoles(t.Context(), syntax.DID(org), alice)
		require.NoError(t, err)
		require.NotEmpty(t, roles)
		code, out = join(org, "")
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "admitted", out.Status)
	})

	t.Run("an invitation admits its holder to an invite-only group", func(t *testing.T) {
		org := newGroup(t, "closed-group", opensocial.JoinPolicyInvite)
		code, _ := join(org, "")
		require.Equal(t, http.StatusForbidden, code)
		code, _ = join(org, "nosuchinvite")
		require.Equal(t, http.StatusBadRequest, code)

		invite, err := adminTS.OpenSocialStore.CreateInvite(
			t.Context(), syntax.DID(org), alice, []string{opensocial.MemberRoleRkey},
		)
		require.NoError(t, err)
		code, out := join(org, invite.ID)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "admitted", out.Status)
	})

	t.Run("a group that does not exist is not found", func(t *testing.T) {
		code, _ := join("did:plc:nogroup", "")
		require.Equal(t, http.StatusNotFound, code)
	})
}
