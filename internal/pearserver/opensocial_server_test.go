package pearserver_test

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
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/fgastore"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
)

func newOpenSocialServer(t *testing.T, did syntax.DID) *pearserver_testutil.TestServer {
	t.Helper()
	return pearserver_testutil.NewTestServer(
		t,
		pearserver_testutil.WithValidator(
			authntest.NewSuccessValidator(&authn.CredentialInfo{Subject: did}),
		),
	)
}

// newSharedOpenSocialServers returns two TestServers (authenticated as admin
// and alice) that share the same backing stores, so invites created by the
// admin are visible to alice.
func newSharedOpenSocialServers(
	t *testing.T,
) (*pearserver_testutil.TestServer, *pearserver_testutil.TestServer, spaces.Store) {
	t.Helper()
	db := db_testutil.NewDB(t)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })
	sp := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(db), spaces_testutil.WithFGA(fga))

	adminTS := pearserver_testutil.NewTestServer(
		t,
		pearserver_testutil.WithValidator(
			authntest.NewSuccessValidator(&authn.CredentialInfo{Subject: admin}),
		),
		pearserver_testutil.WithDB(db),
		pearserver_testutil.WithFGA(fga),
		pearserver_testutil.WithSpaceStore(sp),
	)
	aliceTS := pearserver_testutil.NewTestServer(
		t,
		pearserver_testutil.WithValidator(
			authntest.NewSuccessValidator(&authn.CredentialInfo{Subject: alice}),
		),
		pearserver_testutil.WithDB(db),
		pearserver_testutil.WithFGA(fga),
		pearserver_testutil.WithSpaceStore(sp),
	)
	return adminTS, aliceTS, sp
}

// bootstrapAdminMemberships creates the org's members space with `admin`
// holding the admin role, mirroring the subset of NewOrg's setup the invite
// flow depends on.
func bootstrapAdminMemberships(t *testing.T, sp spaces.Store) {
	t.Helper()
	membersSpace, err := sp.CreateSpace(
		t.Context(), org, "community.opensocial.members", "self",
	)
	require.NoError(t, err)
	_, _, err = sp.PutRecord(
		t.Context(), membersSpace, org, "community.opensocial.membership",
		syntax.RecordKey(admin),
		spaces_testutil.MustMarshalRecord(
			t,
			opensocial_api.CommunityOpensocialMembership{Roles: []string{opensocial.AdminRoleRkey}},
		),
	)
	require.NoError(t, err)
}

func TestServer_CreateOrg(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("creates org and makes caller admin", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)

		var out habitat.NetworkHabitatOpensocialCreateOrgOutput
		code := client.Procedure(
			ts.Server.CreateOrg,
			habitat.NetworkHabitatOpensocialCreateOrgInput{Handle: "acme"},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
		require.NotEmpty(t, out.Org)

		roles, err := ts.OpenSocialStore.GetUserRoles(t.Context(), syntax.DID(out.Org), alice)
		require.NoError(t, err)
		require.Equal(t, []string{opensocial.AdminRoleRkey}, roles)
	})

	t.Run("requires auth", func(t *testing.T) {
		ts := pearserver_testutil.NewTestServer(t,
			pearserver_testutil.WithValidator(authntest.NewFailureValidator()),
		)

		var out habitat.NetworkHabitatOpensocialCreateOrgOutput
		code := client.Procedure(
			ts.Server.CreateOrg,
			habitat.NetworkHabitatOpensocialCreateOrgInput{Handle: "acme"},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})
}

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

func TestServer_Invites(t *testing.T) {
	adminTS, aliceTS, sp := newSharedOpenSocialServers(t)
	bootstrapAdminMemberships(t, sp)
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("create invite requires admin", func(t *testing.T) {
		var out opensocial_api.CommunityOpensocialCreateInviteOutput
		code := client.Procedure(
			aliceTS.Server.CreateInvite,
			opensocial_api.CommunityOpensocialCreateInviteInput{
				Org: org.String(), Invitee: alice.String(),
			},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("invite flow", func(t *testing.T) {
		// Admin invites alice.
		var createOut opensocial_api.CommunityOpensocialCreateInviteOutput
		createCode := client.Procedure(
			adminTS.Server.CreateInvite,
			opensocial_api.CommunityOpensocialCreateInviteInput{
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
		var pendingOut opensocial_api.CommunityOpensocialListPendingInvitesOutput
		pendingCode := client.Query(
			adminTS.Server.ListPendingInvites, pendingParams, &pendingOut,
		)
		require.Equal(t, http.StatusOK, pendingCode)
		require.Len(t, pendingOut.Invites, 1)

		var forbiddenOut opensocial_api.CommunityOpensocialListPendingInvitesOutput
		forbiddenCode := client.Query(
			aliceTS.Server.ListPendingInvites, pendingParams, &forbiddenOut,
		)
		require.Equal(t, http.StatusUnauthorized, forbiddenCode)

		// Alice sees the invite addressed to her.
		listParams := url.Values{"org": []string{org.String()}}
		var listOut opensocial_api.CommunityOpensocialListInvitesOutput
		listCode := client.Query(
			aliceTS.Server.ListInvites, listParams, &listOut,
		)
		require.Equal(t, http.StatusOK, listCode)
		require.Len(t, listOut.Invites, 1)

		// It also shows up in her cross-org inbox, with the org attached.
		var myInvitesOut opensocial_api.CommunityOpensocialListInvitesOutput
		myInvitesCode := client.Query(
			aliceTS.Server.ListInvites, url.Values{}, &myInvitesOut,
		)
		require.Equal(t, http.StatusOK, myInvitesCode)
		require.Len(t, myInvitesOut.Invites, 1)
		require.Equal(t, org.String(), myInvitesOut.Invites[0].Org)

		// Alice accepts.
		var joinOut opensocial_api.CommunityOpensocialRequestJoinOutput
		joinCode := client.Procedure(
			aliceTS.Server.RequestJoin,
			opensocial_api.CommunityOpensocialRequestJoinInput{Org: org.String()},
			&joinOut,
		)
		require.Equal(t, http.StatusOK, joinCode)
		require.Equal(t, []string{opensocial.MemberRoleRkey}, joinOut.Roles)

		// The invite is consumed, but alice is now a member, so requestJoin is
		// idempotent: it confirms her existing membership rather than erroring.
		var rejoinOut opensocial_api.CommunityOpensocialRequestJoinOutput
		rejoinCode := client.Procedure(
			aliceTS.Server.RequestJoin,
			opensocial_api.CommunityOpensocialRequestJoinInput{Org: org.String()},
			&rejoinOut,
		)
		require.Equal(t, http.StatusOK, rejoinCode)
		require.Equal(t, []string{opensocial.MemberRoleRkey}, rejoinOut.Roles)
	})

	t.Run("revoke invite", func(t *testing.T) {
		// Bob, not alice: alice is already a member from the "invite flow" subtest above.
		var createOut opensocial_api.CommunityOpensocialCreateInviteOutput
		client.Procedure(
			adminTS.Server.CreateInvite,
			opensocial_api.CommunityOpensocialCreateInviteInput{
				Org: org.String(), Invitee: bob.String(),
			},
			&createOut,
		)

		var revokeOut struct{}
		revokeCode := client.Procedure(
			adminTS.Server.RevokeInvite,
			opensocial_api.CommunityOpensocialRevokeInviteInput{
				Org: org.String(), Id: createOut.Invite.Id,
			},
			&revokeOut,
		)
		require.Equal(t, http.StatusOK, revokeCode)

		var revokeAgainOut struct{}
		revokeAgainCode := client.Procedure(
			adminTS.Server.RevokeInvite,
			opensocial_api.CommunityOpensocialRevokeInviteInput{
				Org: org.String(), Id: createOut.Invite.Id,
			},
			&revokeAgainOut,
		)
		require.Equal(t, http.StatusNotFound, revokeAgainCode)
	})
}
