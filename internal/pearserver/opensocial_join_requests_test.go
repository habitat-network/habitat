package pearserver_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
)

func TestServer_GroupJoinRequests(t *testing.T) {
	adminTS, aliceTS, _ := newSharedOpenSocialServers(t)
	org, err := adminTS.OpenSocialStore.NewOrg(t.Context(), "jr-acme", admin)
	require.NoError(t, err)
	group := syntax.DID(org)
	require.NoError(t, adminTS.OpenSocialStore.UpdateProfile(
		t.Context(), group, "jr-acme", "", "approval",
	))
	client := httpx_testutil.NewTestXRPCClient(t)
	bob := syntax.DID("did:plc:bob-jr")

	var pending opensocial_api.GroupOpensocialRequestJoinOutput
	require.Equal(t, http.StatusOK, client.Procedure(aliceTS.Server.GroupRequestJoin,
		opensocial_api.GroupOpensocialRequestJoinInput{Group: org, Message: "hello"}, &pending))
	require.Equal(t, "pending", pending.Status)
	_, err = adminTS.OpenSocialStore.CreateJoinRequest(t.Context(), group, bob, "me too")
	require.NoError(t, err)

	t.Run("a member without the scope cannot list requests", func(t *testing.T) {
		var out opensocial_api.GroupOpensocialListJoinRequestsOutput
		require.Equal(t, http.StatusUnauthorized, client.Query(
			aliceTS.Server.GroupListJoinRequests, url.Values{"group": {org}}, &out,
		))
	})

	t.Run("an admin lists requests a page at a time", func(t *testing.T) {
		var first opensocial_api.GroupOpensocialListJoinRequestsOutput
		require.Equal(t, http.StatusOK, client.Query(
			adminTS.Server.GroupListJoinRequests,
			url.Values{"group": {org}, "limit": {"1"}}, &first,
		))
		require.Len(t, first.Requests, 1)
		require.NotEmpty(t, first.Cursor)

		var second opensocial_api.GroupOpensocialListJoinRequestsOutput
		require.Equal(t, http.StatusOK, client.Query(
			adminTS.Server.GroupListJoinRequests,
			url.Values{"group": {org}, "limit": {"1"}, "cursor": {first.Cursor}}, &second,
		))
		require.Len(t, second.Requests, 1)
		require.NotEqual(t, first.Requests[0].Member, second.Requests[0].Member)

		var bad opensocial_api.GroupOpensocialListJoinRequestsOutput
		require.Equal(t, http.StatusBadRequest, client.Query(
			adminTS.Server.GroupListJoinRequests,
			url.Values{"group": {org}, "cursor": {"!!"}}, &bad,
		))
		require.Equal(t, http.StatusBadRequest, client.Query(
			adminTS.Server.GroupListJoinRequests,
			url.Values{"group": {org}, "limit": {"0"}}, &bad,
		))
	})

	t.Run("a requester cancels their own request", func(t *testing.T) {
		in := opensocial_api.GroupOpensocialCancelJoinRequestInput{Group: org}
		require.Equal(t, http.StatusOK,
			client.Procedure(aliceTS.Server.GroupCancelJoinRequest, in, &struct{}{}))
		require.Equal(t, http.StatusNotFound,
			client.Procedure(aliceTS.Server.GroupCancelJoinRequest, in, &struct{}{}))
		require.Equal(t, http.StatusNotFound, client.Procedure(
			aliceTS.Server.GroupCancelJoinRequest,
			opensocial_api.GroupOpensocialCancelJoinRequestInput{Group: "did:plc:nowhere"},
			&struct{}{},
		))
	})

	t.Run("an admin denies a request, and only an admin may", func(t *testing.T) {
		in := opensocial_api.GroupOpensocialDenyJoinRequestInput{Member: string(bob)}
		body := map[string]any{"group": org, "member": in.Member}
		require.Equal(t, http.StatusUnauthorized,
			client.Procedure(aliceTS.Server.GroupDenyJoinRequest, body, &struct{}{}))
		require.Equal(t, http.StatusOK,
			client.Procedure(adminTS.Server.GroupDenyJoinRequest, body, &struct{}{}))
		require.Equal(t, http.StatusNotFound,
			client.Procedure(adminTS.Server.GroupDenyJoinRequest, body, &struct{}{}))
	})

	t.Run("approving a request by admitting the member clears it", func(t *testing.T) {
		_, err := adminTS.OpenSocialStore.CreateJoinRequest(t.Context(), group, bob, "again")
		require.NoError(t, err)
		require.NoError(t, adminTS.OpenSocialStore.AssignRoles(
			t.Context(), group, bob, []string{opensocial.MemberRoleRkey},
		))
		var out opensocial_api.GroupOpensocialListJoinRequestsOutput
		require.Equal(t, http.StatusOK, client.Query(
			adminTS.Server.GroupListJoinRequests, url.Values{"group": {org}}, &out,
		))
		require.Empty(t, out.Requests)
	})
}
