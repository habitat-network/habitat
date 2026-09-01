package server_test

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
	opensocial_server "github.com/habitat-network/habitat/internal/opensocial/server"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
)

var (
	orgDID = syntax.DID("did:plc:org")
	admin  = syntax.DID("did:plc:admin")
	alice  = syntax.DID("did:plc:alice")
)

func successValidator(did syntax.DID) authn.RequestValidator {
	return authntest.NewSuccessValidator(&authn.CredentialInfo{Subject: did})
}

// newTestStore bootstraps an org's members space with `admin` holding the
// admin role.
func newTestStore(t *testing.T) opensocial.Store {
	t.Helper()
	store, spacesStore := opensocial_testutil.NewTestStore(t)

	membersSpace, err := spacesStore.CreateSpace(
		t.Context(), orgDID, "community.opensocial.members", "self",
	)
	require.NoError(t, err)
	_, _, err = spacesStore.PutRecord(
		t.Context(), membersSpace, orgDID, "community.opensocial.membership",
		syntax.RecordKey(admin),
		opensocial_api.CommunityOpensocialMembership{Roles: []string{opensocial.AdminRoleRkey}},
	)
	require.NoError(t, err)

	return store
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
	store, _ := opensocial_testutil.NewTestStore(t)
	s := opensocial_server.NewServer(store, successValidator(alice))

	w := doJSON(t, s.CreateOrg, http.MethodPost, "/xrpc/network.habitat.opensocial.createOrg",
		habitat.NetworkHabitatOpensocialCreateOrgInput{Handle: "acme"})
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatOpensocialCreateOrgOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.NotEmpty(t, out.Org)

	// The caller was bootstrapped as the new org's admin.
	roles, err := store.GetUserRoles(t.Context(), syntax.DID(out.Org), alice)
	require.NoError(t, err)
	require.Equal(t, []string{opensocial.AdminRoleRkey}, roles)
}

func TestServer_CreateOrg_RequiresAuth(t *testing.T) {
	store, _ := opensocial_testutil.NewTestStore(t)
	s := opensocial_server.NewServer(store, authntest.NewFailureValidator())

	w := doJSON(t, s.CreateOrg, http.MethodPost, "/xrpc/network.habitat.opensocial.createOrg",
		habitat.NetworkHabitatOpensocialCreateOrgInput{Handle: "acme"})
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestServer_UpdateProfile(t *testing.T) {
	store, _ := opensocial_testutil.NewTestStore(t)
	orgDIDStr, err := store.NewOrg(t.Context(), "acme", admin)
	require.NoError(t, err)
	org := orgDIDStr

	adminServer := opensocial_server.NewServer(store, successValidator(admin))
	aliceServer := opensocial_server.NewServer(store, successValidator(alice))

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
	s := opensocial_server.NewServer(newTestStore(t), successValidator(alice))

	w := doJSON(t, s.CreateInvite, http.MethodPost, "/xrpc/community.opensocial.createInvite",
		opensocial_api.CommunityOpensocialCreateInviteInput{
			Org: orgDID.String(), Invitee: alice.String(),
		})

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestServer_InviteFlow(t *testing.T) {
	store := newTestStore(t)
	adminServer := opensocial_server.NewServer(store, successValidator(admin))
	aliceServer := opensocial_server.NewServer(store, successValidator(alice))

	// Admin invites alice.
	createW := doJSON(
		t, adminServer.CreateInvite, http.MethodPost, "/xrpc/community.opensocial.createInvite",
		opensocial_api.CommunityOpensocialCreateInviteInput{
			Org:     orgDID.String(),
			Invitee: alice.String(),
			Roles:   []string{opensocial.MemberRoleRkey},
		},
	)
	require.Equal(t, http.StatusOK, createW.Code)
	var createOut opensocial_api.CommunityOpensocialCreateInviteOutput
	require.NoError(t, json.NewDecoder(createW.Body).Decode(&createOut))
	require.Equal(t, alice.String(), createOut.Invite.Invitee)

	// Admin sees it in the pending list; alice, not being an admin, can't.
	pendingPath := "/xrpc/community.opensocial.listPendingInvites?org=" +
		url.QueryEscape(orgDID.String())
	pendingW := doGet(t, adminServer.ListPendingInvites, pendingPath)
	require.Equal(t, http.StatusOK, pendingW.Code)
	var pendingOut opensocial_api.CommunityOpensocialListPendingInvitesOutput
	require.NoError(t, json.NewDecoder(pendingW.Body).Decode(&pendingOut))
	require.Len(t, pendingOut.Invites, 1)

	forbiddenW := doGet(t, aliceServer.ListPendingInvites, pendingPath)
	require.Equal(t, http.StatusUnauthorized, forbiddenW.Code)

	// Alice sees the invite addressed to her.
	listPath := "/xrpc/community.opensocial.listInvites?org=" + url.QueryEscape(orgDID.String())
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
	require.Equal(t, orgDID.String(), myInvitesOut.Invites[0].Org)

	// Alice accepts.
	joinW := doJSON(
		t, aliceServer.RequestJoin, http.MethodPost, "/xrpc/community.opensocial.requestJoin",
		opensocial_api.CommunityOpensocialRequestJoinInput{Org: orgDID.String()},
	)
	require.Equal(t, http.StatusOK, joinW.Code)
	var joinOut opensocial_api.CommunityOpensocialRequestJoinOutput
	require.NoError(t, json.NewDecoder(joinW.Body).Decode(&joinOut))
	require.Equal(t, []string{opensocial.MemberRoleRkey}, joinOut.Roles)

	// The invite is consumed, but alice is now a member, so requestJoin is
	// idempotent: it confirms her existing membership rather than erroring.
	rejoinW := doJSON(
		t, aliceServer.RequestJoin, http.MethodPost, "/xrpc/community.opensocial.requestJoin",
		opensocial_api.CommunityOpensocialRequestJoinInput{Org: orgDID.String()},
	)
	require.Equal(t, http.StatusOK, rejoinW.Code)
	var rejoinOut opensocial_api.CommunityOpensocialRequestJoinOutput
	require.NoError(t, json.NewDecoder(rejoinW.Body).Decode(&rejoinOut))
	require.Equal(t, []string{opensocial.MemberRoleRkey}, rejoinOut.Roles)
}

func TestServer_RevokeInvite(t *testing.T) {
	s := opensocial_server.NewServer(newTestStore(t), successValidator(admin))

	createW := doJSON(
		t, s.CreateInvite, http.MethodPost, "/xrpc/community.opensocial.createInvite",
		opensocial_api.CommunityOpensocialCreateInviteInput{
			Org: orgDID.String(), Invitee: alice.String(),
		},
	)
	require.Equal(t, http.StatusOK, createW.Code)
	var createOut opensocial_api.CommunityOpensocialCreateInviteOutput
	require.NoError(t, json.NewDecoder(createW.Body).Decode(&createOut))

	revokeW := doJSON(
		t, s.RevokeInvite, http.MethodPost, "/xrpc/community.opensocial.revokeInvite",
		opensocial_api.CommunityOpensocialRevokeInviteInput{
			Org: orgDID.String(), Id: createOut.Invite.Id,
		},
	)
	require.Equal(t, http.StatusOK, revokeW.Code)

	revokeAgainW := doJSON(
		t, s.RevokeInvite, http.MethodPost, "/xrpc/community.opensocial.revokeInvite",
		opensocial_api.CommunityOpensocialRevokeInviteInput{
			Org: orgDID.String(), Id: createOut.Invite.Id,
		},
	)
	require.Equal(t, http.StatusNotFound, revokeAgainW.Code)
}
