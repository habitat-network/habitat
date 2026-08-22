package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/instance"
	orgpkg "github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pearsetup/testutil"
)

// decodeBody decodes a response body regardless of status, for assertions on
// non-200 responses that Query/Procedure don't decode automatically.
func decodeBody(resp *http.Response, out any) error {
	return json.NewDecoder(resp.Body).Decode(out)
}

// policyStore returns p's instance store as an instance.PolicyStore. Pear
// exposes InstanceStore as an instance.AdminStore since that's what routes
// need; the same underlying store also satisfies PolicyStore, which is what
// invite validation needs here.
func policyStore(t *testing.T, p *testutil.TestPear) instance.PolicyStore {
	t.Helper()
	ps, ok := p.InstanceStore.(instance.PolicyStore)
	require.True(t, ok, "instance store must also implement PolicyStore")
	return ps
}

func TestIssueTokenThenMintIdentity(t *testing.T) {
	p := testutil.New(t)
	admin := p.NewOrg("acme")

	var issueOut habitat.NetworkHabitatOrgIssueInviteTokenOutput
	issueResp := p.Procedure(admin, "network.habitat.org.issueInviteToken", habitat.NetworkHabitatOrgIssueInviteTokenInput{
		ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339),
	}, &issueOut)
	require.Equal(t, http.StatusOK, issueResp.StatusCode)
	require.NotEmpty(t, issueOut.Token)

	var mintOut habitat.NetworkHabitatOrgMintMemberIdentityOutput
	mintResp := p.Procedure(nil, "network.habitat.org.mintMemberIdentity", habitat.NetworkHabitatOrgMintMemberIdentityInput{
		OrgId:    admin.Org.String(),
		Token:    issueOut.Token,
		Password: "password",
		Handle:   "alice",
	}, &mintOut)
	require.Equal(t, http.StatusOK, mintResp.StatusCode)
	require.NotEmpty(t, mintOut.Did)
	require.NotEmpty(t, mintOut.Handle)

	newMemberDID, err := syntax.ParseDID(mintOut.Did)
	require.NoError(t, err)

	org, err := p.OrgStore.GetOrg(t.Context(), admin.Org)
	require.NoError(t, err)
	members, err := org.GetMembers(t.Context())
	require.NoError(t, err)
	require.Len(t, members, 2)
	require.Contains(t, members, admin.DID, "contains the admin")
	require.Contains(t, members, newMemberDID, "contains the new member")
}

// GetMetadata supports two auth methods:
//  1. orgID in query params + an org-signed token in the Authorization header
//  2. a regular authenticated caller (no orgID), resolved to their org
func TestGetMetadataViaSignedToken(t *testing.T) {
	p := testutil.New(t)
	admin := p.NewOrg("acme")

	// Mint an org-signed token to authenticate the request — distinct from an
	// OAuth actor token, so it's driven through the store directly and sent
	// as a raw bearer token.
	token, err := p.OrgStore.IssueIdentityToken(
		t.Context(), admin.Org, admin.DID, true, time.Now().Add(time.Hour),
	)
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.org.getMetadata?OrgId="+admin.Org.String(),
		http.NoBody,
	)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := p.Do(nil, req)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out habitat.NetworkHabitatOrgGetMetadataOutput
	require.NoError(t, decodeBody(resp, &out))
	require.Equal(t, admin.Org.String(), out.OrgId)
	require.Equal(t, "acme", out.Name)
	require.Equal(t, string(orgpkg.LoginMethodPassword), out.LoginMethod)
}

func TestGetMetadataViaSignedToken_InvalidToken(t *testing.T) {
	p := testutil.New(t)
	admin := p.NewOrg("acme")

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.org.getMetadata?OrgId="+admin.Org.String(),
		http.NoBody,
	)
	req.Header.Set("Authorization", "Bearer not-a-valid-token")
	resp := p.Do(nil, req)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestGetMetadataViaAuthenticatedCaller(t *testing.T) {
	p := testutil.New(t)
	admin := p.NewOrg("acme")

	var out habitat.NetworkHabitatOrgGetMetadataOutput
	resp := p.Query(admin, "network.habitat.org.getMetadata", url.Values{}, &out)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, admin.Org.String(), out.OrgId)
	require.Equal(t, "acme", out.Name)
	require.Equal(t, string(orgpkg.LoginMethodPassword), out.LoginMethod)
}

func TestCreateOrg(t *testing.T) {
	p := testutil.New(t)

	var out habitat.NetworkHabitatOrgCreateOutput
	resp := p.Procedure(nil, "network.habitat.org.create", habitat.NetworkHabitatOrgCreateInput{
		Name:            "My Org",
		AdminHandle:     "admin",
		AdminPassword:   "securepassword123",
		LoginMethod:     "password",
		HandleSubdomain: "org",
		ContactEmail:    "contact@example.com",
	}, &out)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NotEmpty(t, out.OrgId)
	require.NotEmpty(t, out.AdminDid)
	require.Contains(t, out.AdminHandle, "admin")
	require.Equal(t, "My Org", out.Name)

	adminDID, err := syntax.ParseDID(out.AdminDid)
	require.NoError(t, err)
	orgID, err := syntax.ParseDID(out.OrgId)
	require.NoError(t, err)

	org, err := p.OrgStore.GetOrg(t.Context(), orgID)
	require.NoError(t, err)
	members, err := org.GetMembers(t.Context())
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.Equal(t, adminDID, members[0])

	admins, err := org.GetAdmins(t.Context())
	require.NoError(t, err)
	require.Len(t, admins, 1)
	require.Equal(t, adminDID, admins[0])
}

func TestCreateOrg_InvalidHandle(t *testing.T) {
	p := testutil.New(t)

	resp := p.Procedure(nil, "network.habitat.org.create", habitat.NetworkHabitatOrgCreateInput{
		AdminHandle:     "invalid handle with spaces!",
		AdminPassword:   "password",
		LoginMethod:     "password",
		HandleSubdomain: "org",
		ContactEmail:    "contact@example.com",
	}, nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCreateOrg_ContactEmailValidation(t *testing.T) {
	tests := []struct {
		name         string
		contactEmail string
	}{
		{name: "missing", contactEmail: ""},
		{name: "malformed", contactEmail: "not-an-email"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := testutil.New(t)
			resp := p.Procedure(nil, "network.habitat.org.create", habitat.NetworkHabitatOrgCreateInput{
				Name:            "My Org",
				AdminHandle:     "admin",
				AdminPassword:   "securepassword123",
				LoginMethod:     "password",
				HandleSubdomain: "org",
				ContactEmail:    tt.contactEmail,
			}, nil)
			require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	}
}

func TestCreateOrg_MissingFields(t *testing.T) {
	p := testutil.New(t)

	resp := p.Procedure(nil, "network.habitat.org.create", habitat.NetworkHabitatOrgCreateInput{
		AdminHandle:     "admin",
		HandleSubdomain: "org",
		ContactEmail:    "contact@example.com",
	}, nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCreateOrg_OpenPolicyIgnoresMissingToken(t *testing.T) {
	p := testutil.New(t)
	require.NoError(t, p.InstanceStore.UpdateSettings(t.Context(), "Acme Hosting", "open"))

	resp := p.Procedure(nil, "network.habitat.org.create", habitat.NetworkHabitatOrgCreateInput{
		Name:            "test-org",
		AdminHandle:     "admin",
		AdminPassword:   "password",
		LoginMethod:     "password",
		HandleSubdomain: "test",
		ContactEmail:    "contact@example.com",
	}, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestCreateOrg_InviteOnlyRejectsMissingToken(t *testing.T) {
	p := testutil.New(t)
	require.NoError(t, p.InstanceStore.UpdateSettings(t.Context(), "Acme Hosting", "invite_only"))

	resp := p.Procedure(nil, "network.habitat.org.create", habitat.NetworkHabitatOrgCreateInput{
		Name:            "test-org",
		AdminHandle:     "admin",
		AdminPassword:   "password",
		LoginMethod:     "password",
		HandleSubdomain: "test",
		ContactEmail:    "contact@example.com",
	}, nil)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCreateOrg_InviteOnlyRejectsInvalidToken(t *testing.T) {
	p := testutil.New(t)
	require.NoError(t, p.InstanceStore.UpdateSettings(t.Context(), "Acme Hosting", "invite_only"))

	resp := p.Procedure(nil, "network.habitat.org.create", habitat.NetworkHabitatOrgCreateInput{
		Name:            "test-org",
		AdminHandle:     "admin",
		AdminPassword:   "password",
		LoginMethod:     "password",
		HandleSubdomain: "test",
		ContactEmail:    "contact@example.com",
		InviteToken:     "garbage",
	}, nil)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCreateOrg_InviteOnlyAcceptsValidToken(t *testing.T) {
	p := testutil.New(t)
	require.NoError(t, p.InstanceStore.UpdateSettings(t.Context(), "Acme Hosting", "invite_only"))
	token, err := p.InstanceStore.IssueInvite(t.Context())
	require.NoError(t, err)

	resp := p.Procedure(nil, "network.habitat.org.create", habitat.NetworkHabitatOrgCreateInput{
		Name:            "test-org",
		AdminHandle:     "admin",
		AdminPassword:   "password",
		LoginMethod:     "password",
		HandleSubdomain: "test",
		ContactEmail:    "contact@example.com",
		InviteToken:     token,
	}, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// The real invite should now be consumed - validating it again should fail.
	require.ErrorIs(t, policyStore(t, p).ValidateInvite(t.Context(), token), instance.ErrInvalidInvite)
}

func TestCreateOrg_InviteOnlyDoesNotMarkUsedOnCreateFailure(t *testing.T) {
	p := testutil.New(t)
	require.NoError(t, p.InstanceStore.UpdateSettings(t.Context(), "Acme Hosting", "invite_only"))

	// Pre-create an org using the same handle subdomain so the subsequent
	// CreateOrg call fails (the hive identity mint for that subdomain
	// collides, surfacing as a generic creation failure — the precise
	// status code isn't the point here, only that CreateOrg fails and the
	// invite must not be marked used in that case).
	_, _, err := p.OrgStore.CreateOrg(
		t.Context(), "existing-org", "admin", "password", "password", "", "test", "contact@example.com",
	)
	require.NoError(t, err)

	token, err := p.InstanceStore.IssueInvite(t.Context())
	require.NoError(t, err)

	resp := p.Procedure(nil, "network.habitat.org.create", habitat.NetworkHabitatOrgCreateInput{
		Name:            "test-org",
		AdminHandle:     "admin",
		AdminPassword:   "password",
		LoginMethod:     "password",
		HandleSubdomain: "test",
		ContactEmail:    "contact@example.com",
		InviteToken:     token,
	}, nil)
	require.NotEqual(t, http.StatusOK, resp.StatusCode)

	// The invite must still validate — it was never marked used.
	require.NoError(t, policyStore(t, p).ValidateInvite(t.Context(), token))
}

func TestAddAdminRejectsNonAdmin(t *testing.T) {
	p := testutil.New(t)
	admin := p.NewOrg("acme")
	member := p.NewMember(admin, "alice")
	target := p.NewMember(admin, "bob")

	resp := p.Procedure(member, "network.habitat.org.addAdmin", habitat.NetworkHabitatOrgAddAdminInput{
		Admin: target.DID.String(),
	}, nil)
	require.NotEqual(t, http.StatusOK, resp.StatusCode,
		"a plain member must not be able to promote an admin")
}

func TestAddAdminAllowsAdmin(t *testing.T) {
	p := testutil.New(t)
	admin := p.NewOrg("acme")
	target := p.NewMember(admin, "alice")

	resp := p.Procedure(admin, "network.habitat.org.addAdmin", habitat.NetworkHabitatOrgAddAdminInput{
		Admin: target.DID.String(),
	}, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	org, err := p.OrgStore.GetOrg(t.Context(), admin.Org)
	require.NoError(t, err)
	isAdmin, err := org.IsAdmin(t.Context(), target.DID)
	require.NoError(t, err)
	require.True(t, isAdmin)
}

func TestGetMembersRejectsAnonymous(t *testing.T) {
	p := testutil.New(t)
	p.NewOrg("acme")

	resp := p.Query(p.Anonymous(), "network.habitat.org.getMembers", url.Values{}, nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
