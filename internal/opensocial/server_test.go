package opensocial_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
)

var (
	serverOrgDID = syntax.DID("did:plc:org")
	serverAdmin  = syntax.DID("did:plc:admin")
	serverAlice  = syntax.DID("did:plc:alice")
	serverBob    = syntax.DID("did:plc:bob")
)

func successValidator(did syntax.DID) authn.RequestValidator {
	return authntest.NewSuccessValidator(&authn.CredentialInfo{Subject: did})
}

// newServerStore bootstraps an org's members space with `serverAdmin` holding the
// admin role.
func newServerStore(t *testing.T) *opensocial_testutil.TestStore {
	t.Helper()
	ts := opensocial_testutil.NewTestStore(t)

	membersSpace, err := ts.SpaceStore.CreateSpace(
		t.Context(), serverOrgDID, "community.opensocial.members", "self",
	)
	require.NoError(t, err)
	_, _, err = ts.SpaceStore.PutRecord(
		t.Context(), membersSpace, serverOrgDID, "community.opensocial.membership",
		syntax.RecordKey(serverAdmin),
		spaces_testutil.MustMarshalRecord(
			t,
			opensocial_api.CommunityOpensocialMembership{Roles: []string{opensocial.AdminRoleRkey}},
		),
	)
	require.NoError(t, err)

	return ts
}

func TestServer_CreateOrg(t *testing.T) {
	ts := opensocial_testutil.NewTestStore(t)
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("creates org and makes caller admin", func(t *testing.T) {
		s := opensocial.NewServer(ts.Store, successValidator(serverAlice))

		var out habitat.NetworkHabitatOpensocialCreateOrgOutput
		code := client.Procedure(
			s.CreateOrg,
			habitat.NetworkHabitatOpensocialCreateOrgInput{Handle: "acme"},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
		require.NotEmpty(t, out.Org)

		roles, err := ts.GetUserRoles(t.Context(), syntax.DID(out.Org), serverAlice)
		require.NoError(t, err)
		require.Equal(t, []string{opensocial.AdminRoleRkey}, roles)
	})

	t.Run("requires auth", func(t *testing.T) {
		s := opensocial.NewServer(ts.Store, authntest.NewFailureValidator())

		var out habitat.NetworkHabitatOpensocialCreateOrgOutput
		code := client.Procedure(
			s.CreateOrg,
			habitat.NetworkHabitatOpensocialCreateOrgInput{Handle: "acme"},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})
}

func TestServer_UpdateProfile(t *testing.T) {
	ts := opensocial_testutil.NewTestStore(t)
	org, err := ts.NewOrg(t.Context(), "acme", serverAdmin)
	require.NoError(t, err)
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("requires admin", func(t *testing.T) {
		aliceServer := opensocial.NewServer(ts.Store, successValidator(serverAlice))

		var out struct{}
		code := client.Procedure(
			aliceServer.UpdateProfile,
			opensocial_api.CommunityOpensocialUpdateProfileInput{
				Org: org, Name: "Acme Corp",
			},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("updates profile", func(t *testing.T) {
		adminServer := opensocial.NewServer(ts.Store, successValidator(serverAdmin))

		var out struct{}
		code := client.Procedure(
			adminServer.UpdateProfile,
			opensocial_api.CommunityOpensocialUpdateProfileInput{
				Org: org, Name: "Acme Corp", Description: "We make widgets",
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
	})
}

func TestServer_Invites(t *testing.T) {
	store := newServerStore(t)
	adminServer := opensocial.NewServer(store.Store, successValidator(serverAdmin))
	aliceServer := opensocial.NewServer(store.Store, successValidator(serverAlice))
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("create invite requires admin", func(t *testing.T) {
		var out opensocial_api.CommunityOpensocialCreateInviteOutput
		code := client.Procedure(
			aliceServer.CreateInvite,
			opensocial_api.CommunityOpensocialCreateInviteInput{
				Org: serverOrgDID.String(), Invitee: serverAlice.String(),
			},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("invite flow", func(t *testing.T) {
		// Admin invites alice.
		var createOut opensocial_api.CommunityOpensocialCreateInviteOutput
		createCode := client.Procedure(
			adminServer.CreateInvite,
			opensocial_api.CommunityOpensocialCreateInviteInput{
				Org:     serverOrgDID.String(),
				Invitee: serverAlice.String(),
				Roles:   []string{opensocial.MemberRoleRkey},
			},
			&createOut,
		)
		require.Equal(t, http.StatusOK, createCode)
		require.Equal(t, serverAlice.String(), createOut.Invite.Invitee)

		// Admin sees it in the pending list; alice, not being an admin, can't.
		pendingParams := url.Values{"org": []string{serverOrgDID.String()}}
		var pendingOut opensocial_api.CommunityOpensocialListPendingInvitesOutput
		pendingCode := client.Query(
			adminServer.ListPendingInvites, pendingParams, &pendingOut,
		)
		require.Equal(t, http.StatusOK, pendingCode)
		require.Len(t, pendingOut.Invites, 1)

		var forbiddenOut opensocial_api.CommunityOpensocialListPendingInvitesOutput
		forbiddenCode := client.Query(
			aliceServer.ListPendingInvites, pendingParams, &forbiddenOut,
		)
		require.Equal(t, http.StatusUnauthorized, forbiddenCode)

		// Alice sees the invite addressed to her.
		listParams := url.Values{"org": []string{serverOrgDID.String()}}
		var listOut opensocial_api.CommunityOpensocialListInvitesOutput
		listCode := client.Query(
			aliceServer.ListInvites, listParams, &listOut,
		)
		require.Equal(t, http.StatusOK, listCode)
		require.Len(t, listOut.Invites, 1)

		// It also shows up in her cross-org inbox, with the org attached.
		var myInvitesOut opensocial_api.CommunityOpensocialListInvitesOutput
		myInvitesCode := client.Query(
			aliceServer.ListInvites, url.Values{}, &myInvitesOut,
		)
		require.Equal(t, http.StatusOK, myInvitesCode)
		require.Len(t, myInvitesOut.Invites, 1)
		require.Equal(t, serverOrgDID.String(), myInvitesOut.Invites[0].Org)

		// Alice accepts.
		var joinOut opensocial_api.CommunityOpensocialRequestJoinOutput
		joinCode := client.Procedure(
			aliceServer.RequestJoin,
			opensocial_api.CommunityOpensocialRequestJoinInput{Org: serverOrgDID.String()},
			&joinOut,
		)
		require.Equal(t, http.StatusOK, joinCode)
		require.Equal(t, []string{opensocial.MemberRoleRkey}, joinOut.Roles)

		// The invite is consumed, but alice is now a member, so requestJoin is
		// idempotent: it confirms her existing membership rather than erroring.
		var rejoinOut opensocial_api.CommunityOpensocialRequestJoinOutput
		rejoinCode := client.Procedure(
			aliceServer.RequestJoin,
			opensocial_api.CommunityOpensocialRequestJoinInput{Org: serverOrgDID.String()},
			&rejoinOut,
		)
		require.Equal(t, http.StatusOK, rejoinCode)
		require.Equal(t, []string{opensocial.MemberRoleRkey}, rejoinOut.Roles)
	})

	t.Run("revoke invite", func(t *testing.T) {
		// Bob, not alice: alice is already a member from the "invite flow" subtest above.
		var createOut opensocial_api.CommunityOpensocialCreateInviteOutput
		client.Procedure(
			adminServer.CreateInvite,
			opensocial_api.CommunityOpensocialCreateInviteInput{
				Org: serverOrgDID.String(), Invitee: serverBob.String(),
			},
			&createOut,
		)

		var revokeOut struct{}
		revokeCode := client.Procedure(
			adminServer.RevokeInvite,
			opensocial_api.CommunityOpensocialRevokeInviteInput{
				Org: serverOrgDID.String(), Id: createOut.Invite.Id,
			},
			&revokeOut,
		)
		require.Equal(t, http.StatusOK, revokeCode)

		var revokeAgainOut struct{}
		revokeAgainCode := client.Procedure(
			adminServer.RevokeInvite,
			opensocial_api.CommunityOpensocialRevokeInviteInput{
				Org: serverOrgDID.String(), Id: createOut.Invite.Id,
			},
			&revokeAgainOut,
		)
		require.Equal(t, http.StatusNotFound, revokeAgainCode)
	})
}
