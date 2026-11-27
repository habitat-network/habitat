package pearserver_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
)

// The invite lifecycle (create → list → accept → revoke) runs against a pair
// of TestServers (admin and alice) sharing one store, since invites created by
// the admin must be visible to alice. The subtests are ordered by that shared
// state.
func TestServer_Invites(t *testing.T) {
	adminTS, aliceTS, sp := newSharedOpenSocialServers(t)
	bootstrapAdminMemberships(t, sp)
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("create invite requires admin", func(t *testing.T) {
		var out habitat.NetworkHabitatOpensocialCreateInviteOutput
		code := client.Procedure(
			aliceTS.Server.CreateInvite,
			habitat.NetworkHabitatOpensocialCreateInviteInput{
				Org: org.String(), Invitee: alice.String(),
			},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("invite flow", func(t *testing.T) {
		// Admin invites alice.
		var createOut habitat.NetworkHabitatOpensocialCreateInviteOutput
		createCode := client.Procedure(
			adminTS.Server.CreateInvite,
			habitat.NetworkHabitatOpensocialCreateInviteInput{
				Org:     org.String(),
				Invitee: alice.String(),
				Roles:   []string{opensocial.MemberRoleRkey},
			},
			&createOut,
		)
		require.Equal(t, http.StatusOK, createCode)
		require.Equal(t, alice.String(), createOut.Invite.Invitee)

		// Admin sees it in the pending list; alice, not being an admin, can't.
		pendingParams := url.Values{"org": []string{org.String()}}
		var pendingOut habitat.NetworkHabitatOpensocialListPendingInvitesOutput
		pendingCode := client.Query(
			adminTS.Server.ListPendingInvites, pendingParams, &pendingOut,
		)
		require.Equal(t, http.StatusOK, pendingCode)
		require.Len(t, pendingOut.Invites, 1)

		var forbiddenOut habitat.NetworkHabitatOpensocialListPendingInvitesOutput
		forbiddenCode := client.Query(
			aliceTS.Server.ListPendingInvites, pendingParams, &forbiddenOut,
		)
		require.Equal(t, http.StatusUnauthorized, forbiddenCode)

		// Alice sees the invite addressed to her.
		listParams := url.Values{"org": []string{org.String()}}
		var listOut habitat.NetworkHabitatOpensocialListInvitesOutput
		listCode := client.Query(
			aliceTS.Server.ListInvites, listParams, &listOut,
		)
		require.Equal(t, http.StatusOK, listCode)
		require.Len(t, listOut.Invites, 1)

		// It also shows up in her cross-org inbox, with the org attached.
		var myInvitesOut habitat.NetworkHabitatOpensocialListInvitesOutput
		myInvitesCode := client.Query(
			aliceTS.Server.ListInvites, url.Values{}, &myInvitesOut,
		)
		require.Equal(t, http.StatusOK, myInvitesCode)
		require.Len(t, myInvitesOut.Invites, 1)
		require.Equal(t, org.String(), myInvitesOut.Invites[0].Org)

		// Alice accepts.
		var joinOut habitat.NetworkHabitatOpensocialRequestJoinOutput
		joinCode := client.Procedure(
			aliceTS.Server.RequestJoin,
			habitat.NetworkHabitatOpensocialRequestJoinInput{Org: org.String()},
			&joinOut,
		)
		require.Equal(t, http.StatusOK, joinCode)
		require.Equal(t, []string{opensocial.MemberRoleRkey}, joinOut.Roles)

		// The invite is consumed, but alice is now a member, so requestJoin is
		// idempotent: it confirms her existing membership rather than erroring.
		var rejoinOut habitat.NetworkHabitatOpensocialRequestJoinOutput
		rejoinCode := client.Procedure(
			aliceTS.Server.RequestJoin,
			habitat.NetworkHabitatOpensocialRequestJoinInput{Org: org.String()},
			&rejoinOut,
		)
		require.Equal(t, http.StatusOK, rejoinCode)
		require.Equal(t, []string{opensocial.MemberRoleRkey}, rejoinOut.Roles)
	})

	t.Run("revoke invite", func(t *testing.T) {
		// Bob, not alice: alice is already a member from the "invite flow" subtest above.
		var createOut habitat.NetworkHabitatOpensocialCreateInviteOutput
		client.Procedure(
			adminTS.Server.CreateInvite,
			habitat.NetworkHabitatOpensocialCreateInviteInput{
				Org: org.String(), Invitee: bob.String(),
			},
			&createOut,
		)

		var revokeOut struct{}
		revokeCode := client.Procedure(
			adminTS.Server.RevokeInvite,
			habitat.NetworkHabitatOpensocialRevokeInviteInput{
				Org: org.String(), Id: createOut.Invite.Id,
			},
			&revokeOut,
		)
		require.Equal(t, http.StatusOK, revokeCode)

		var revokeAgainOut struct{}
		revokeAgainCode := client.Procedure(
			adminTS.Server.RevokeInvite,
			habitat.NetworkHabitatOpensocialRevokeInviteInput{
				Org: org.String(), Id: createOut.Invite.Id,
			},
			&revokeAgainOut,
		)
		require.Equal(t, http.StatusNotFound, revokeAgainCode)
	})
}
