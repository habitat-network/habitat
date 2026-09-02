package opensocial_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
)

var (
	serverOrgDID = syntax.DID("did:plc:org")
	serverAdmin  = syntax.DID("did:plc:admin")
	serverAlice  = syntax.DID("did:plc:alice")
)

func successValidator(did syntax.DID) authn.RequestValidator {
	return authntest.NewSuccessValidator(&authn.CredentialInfo{Subject: did})
}

// newServerStore bootstraps an org's members space with `serverAdmin` holding the
// admin role.
func newServerStore(t *testing.T) opensocial.Store {
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

	return *ts.Store
}

func doJSON(
	t *testing.T,
	handler http.HandlerFunc,
	method string,
	path string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func doGet(t *testing.T, handler http.HandlerFunc, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func TestServer_CreateOrg(t *testing.T) {
	ts := opensocial_testutil.NewTestStore(t)
	s := opensocial.NewServer(*ts.Store, successValidator(serverAlice))

	w := doJSON(t, s.CreateOrg, http.MethodPost, "/xrpc/network.habitat.opensocial.createOrg",
		habitat.NetworkHabitatOpensocialCreateOrgInput{Handle: "acme"})
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatOpensocialCreateOrgOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.NotEmpty(t, out.Org)

	// The caller was bootstrapped as the new org's admin.
	roles, err := ts.GetUserRoles(t.Context(), syntax.DID(out.Org), serverAlice)
	require.NoError(t, err)
	require.Equal(t, []string{opensocial.AdminRoleRkey}, roles)
}

func TestServer_CreateOrg_RequiresAuth(t *testing.T) {
	ts := opensocial_testutil.NewTestStore(t)
	s := opensocial.NewServer(*ts.Store, authntest.NewFailureValidator())

	w := doJSON(t, s.CreateOrg, http.MethodPost, "/xrpc/network.habitat.opensocial.createOrg",
		habitat.NetworkHabitatOpensocialCreateOrgInput{Handle: "acme"})
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestServer_UpdateProfile(t *testing.T) {
	ts := opensocial_testutil.NewTestStore(t)
	orgDIDStr, err := ts.NewOrg(t.Context(), "acme", serverAdmin)
	require.NoError(t, err)
	org := orgDIDStr

	adminServer := opensocial.NewServer(*ts.Store, successValidator(serverAdmin))
	aliceServer := opensocial.NewServer(*ts.Store, successValidator(serverAlice))

	// Requires the caller to be an admin.
	forbiddenW := doJSON(
		t, aliceServer.UpdateProfile, http.MethodPost, "/xrpc/community.opensocial.updateProfile",
		opensocial_api.CommunityOpensocialUpdateProfileInput{
			Org: org, Name: "Acme Corp",
		},
	)
	require.Equal(t, http.StatusUnauthorized, forbiddenW.Code)

	w := doJSON(
		t, adminServer.UpdateProfile, http.MethodPost, "/xrpc/community.opensocial.updateProfile",
		opensocial_api.CommunityOpensocialUpdateProfileInput{
			Org: org, Name: "Acme Corp", Description: "We make widgets",
		},
	)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestServer_CreateInvite_RequiresAdmin(t *testing.T) {
	s := opensocial.NewServer(newServerStore(t), successValidator(serverAlice))

	w := doJSON(t, s.CreateInvite, http.MethodPost, "/xrpc/community.opensocial.createInvite",
		opensocial_api.CommunityOpensocialCreateInviteInput{
			Org: serverOrgDID.String(), Invitee: serverAlice.String(),
		})

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestServer_InviteFlow(t *testing.T) {
	store := newServerStore(t)
	adminServer := opensocial.NewServer(store, successValidator(serverAdmin))
	aliceServer := opensocial.NewServer(store, successValidator(serverAlice))

	// Admin invites alice.
	createW := doJSON(
		t, adminServer.CreateInvite, http.MethodPost, "/xrpc/community.opensocial.createInvite",
		opensocial_api.CommunityOpensocialCreateInviteInput{
			Org:     serverOrgDID.String(),
			Invitee: serverAlice.String(),
			Roles:   []string{opensocial.MemberRoleRkey},
		},
	)
	require.Equal(t, http.StatusOK, createW.Code)
	var createOut opensocial_api.CommunityOpensocialCreateInviteOutput
	require.NoError(t, json.NewDecoder(createW.Body).Decode(&createOut))
	require.Equal(t, serverAlice.String(), createOut.Invite.Invitee)

	// Admin sees it in the pending list; alice, not being an admin, can't.
	pendingPath := "/xrpc/community.opensocial.listPendingInvites?org=" +
		url.QueryEscape(serverOrgDID.String())
	pendingW := doGet(t, adminServer.ListPendingInvites, pendingPath)
	require.Equal(t, http.StatusOK, pendingW.Code)
	var pendingOut opensocial_api.CommunityOpensocialListPendingInvitesOutput
	require.NoError(t, json.NewDecoder(pendingW.Body).Decode(&pendingOut))
	require.Len(t, pendingOut.Invites, 1)

	forbiddenW := doGet(t, aliceServer.ListPendingInvites, pendingPath)
	require.Equal(t, http.StatusUnauthorized, forbiddenW.Code)

	// Alice sees the invite addressed to her.
	listPath := "/xrpc/community.opensocial.listInvites?org=" + url.QueryEscape(
		serverOrgDID.String(),
	)
	listW := doGet(t, aliceServer.ListInvites, listPath)
	require.Equal(t, http.StatusOK, listW.Code)
	var listOut opensocial_api.CommunityOpensocialListInvitesOutput
	require.NoError(t, json.NewDecoder(listW.Body).Decode(&listOut))
	require.Len(t, listOut.Invites, 1)

	// It also shows up in her cross-org inbox, with the org attached.
	myInvitesW := doGet(
		t, aliceServer.ListInvites, "/xrpc/community.opensocial.listInvites",
	)
	require.Equal(t, http.StatusOK, myInvitesW.Code)
	var myInvitesOut opensocial_api.CommunityOpensocialListInvitesOutput
	require.NoError(t, json.NewDecoder(myInvitesW.Body).Decode(&myInvitesOut))
	require.Len(t, myInvitesOut.Invites, 1)
	require.Equal(t, serverOrgDID.String(), myInvitesOut.Invites[0].Org)

	// Alice accepts.
	joinW := doJSON(
		t, aliceServer.RequestJoin, http.MethodPost, "/xrpc/community.opensocial.requestJoin",
		opensocial_api.CommunityOpensocialRequestJoinInput{Org: serverOrgDID.String()},
	)
	require.Equal(t, http.StatusOK, joinW.Code)
	var joinOut opensocial_api.CommunityOpensocialRequestJoinOutput
	require.NoError(t, json.NewDecoder(joinW.Body).Decode(&joinOut))
	require.Equal(t, []string{opensocial.MemberRoleRkey}, joinOut.Roles)

	// The invite is consumed, but alice is now a member, so requestJoin is
	// idempotent: it confirms her existing membership rather than erroring.
	rejoinW := doJSON(
		t, aliceServer.RequestJoin, http.MethodPost, "/xrpc/community.opensocial.requestJoin",
		opensocial_api.CommunityOpensocialRequestJoinInput{Org: serverOrgDID.String()},
	)
	require.Equal(t, http.StatusOK, rejoinW.Code)
	var rejoinOut opensocial_api.CommunityOpensocialRequestJoinOutput
	require.NoError(t, json.NewDecoder(rejoinW.Body).Decode(&rejoinOut))
	require.Equal(t, []string{opensocial.MemberRoleRkey}, rejoinOut.Roles)
}

func TestServer_RevokeInvite(t *testing.T) {
	s := opensocial.NewServer(newServerStore(t), successValidator(serverAdmin))

	createW := doJSON(
		t, s.CreateInvite, http.MethodPost, "/xrpc/community.opensocial.createInvite",
		opensocial_api.CommunityOpensocialCreateInviteInput{
			Org: serverOrgDID.String(), Invitee: serverAlice.String(),
		},
	)
	require.Equal(t, http.StatusOK, createW.Code)
	var createOut opensocial_api.CommunityOpensocialCreateInviteOutput
	require.NoError(t, json.NewDecoder(createW.Body).Decode(&createOut))

	revokeW := doJSON(
		t, s.RevokeInvite, http.MethodPost, "/xrpc/community.opensocial.revokeInvite",
		opensocial_api.CommunityOpensocialRevokeInviteInput{
			Org: serverOrgDID.String(), Id: createOut.Invite.Id,
		},
	)
	require.Equal(t, http.StatusOK, revokeW.Code)

	revokeAgainW := doJSON(
		t, s.RevokeInvite, http.MethodPost, "/xrpc/community.opensocial.revokeInvite",
		opensocial_api.CommunityOpensocialRevokeInviteInput{
			Org: serverOrgDID.String(), Id: createOut.Invite.Id,
		},
	)
	require.Equal(t, http.StatusNotFound, revokeAgainW.Code)
}
