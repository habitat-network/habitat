package org

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	instance "github.com/habitat-network/habitat/internal/instanceadmin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeInstancePolicy struct {
	policy           instance.InvitePolicy
	validateErr      error
	markUsedErr      error
	validatedTokens  []string
	markedUsedTokens []string
}

func (f *fakeInstancePolicy) GetOrgCreationPolicy(
	ctx context.Context,
) (instance.InvitePolicy, error) {
	return f.policy, nil
}

func (f *fakeInstancePolicy) ValidateInvite(ctx context.Context, token string) error {
	f.validatedTokens = append(f.validatedTokens, token)
	return f.validateErr
}

func (f *fakeInstancePolicy) MarkInviteUsed(ctx context.Context, token string) error {
	f.markedUsedTokens = append(f.markedUsedTokens, token)
	return f.markUsedErr
}

func newTestServer(
	t *testing.T,
	adminDID syntax.DID,
	policy instance.PolicyStore,
) (*Server, syntax.DID) {
	t.Helper()
	storeImpl := newTestStore(t)

	orgIdIdent, _, err := storeImpl.CreateOrg(
		t.Context(),
		"test-org",
		"admin",
		"password",
		"password",
		"",
		"",
	)
	require.NoError(t, err)

	scoped, err := storeImpl.GetOrg(context.Background(), orgIdIdent.DID)
	require.NoError(t, err)
	st := scoped.(*orgImpl)
	require.NoError(t, st.addMemberTx(context.Background(), st.db, adminDID))
	require.NoError(t, st.AddAdmin(context.Background(), adminDID))

	srv, err := NewServer(
		storeImpl,
		authn.NewStubAuthnForTest(adminDID),
		nil,
		"pear.example.com",
		identity.DefaultDirectory(),
		policy,
	)
	require.NoError(t, err)
	return srv, orgIdIdent.DID
}

func TestIssueTokenThenMintIdentity(t *testing.T) {
	srv, orgId := newTestServer(t, did1, &fakeInstancePolicy{policy: "open"})

	// Admin issues an invite token
	issueBody, _ := json.Marshal(habitat.NetworkHabitatOrgIssueInviteTokenInput{
		ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339),
	})
	issueReq := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.org.issueInviteToken",
		bytes.NewReader(issueBody),
	)
	issueReq.Header.Set("Content-Type", "application/json")
	issueW := httptest.NewRecorder()
	srv.IssueInviteToken(issueW, issueReq)
	require.Equal(t, http.StatusOK, issueW.Code)

	var issueOut habitat.NetworkHabitatOrgIssueInviteTokenOutput
	require.NoError(t, json.NewDecoder(issueW.Body).Decode(&issueOut))
	require.NotEmpty(t, issueOut.Token)

	// Someone uses the token to mint an identity
	mintBody, _ := json.Marshal(habitat.NetworkHabitatOrgMintMemberIdentityInput{
		OrgId:    orgId.String(),
		Token:    issueOut.Token,
		Password: "password",
		Handle:   "alice",
	})
	mintReq := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.org.mintMemberIdentity",
		bytes.NewReader(mintBody),
	)
	mintReq.Header.Set("Content-Type", "application/json")
	mintW := httptest.NewRecorder()
	srv.MintMemberIdentity(mintW, mintReq)
	require.Equal(t, http.StatusOK, mintW.Code)

	var mintOut habitat.NetworkHabitatOrgMintMemberIdentityOutput
	require.NoError(t, json.NewDecoder(mintW.Body).Decode(&mintOut))
	require.NotEmpty(t, mintOut.Did)
	require.NotEmpty(t, mintOut.Handle)

	newMemberDID, err := syntax.ParseDID(mintOut.Did)
	require.NoError(t, err)

	testOrg, err := srv.store.GetOrg(context.Background(), orgId)
	require.NoError(t, err)
	members, err := testOrg.GetMembers(context.Background())
	require.NoError(t, err)
	require.Len(t, members, 3)
	require.Contains(t, members, did1, "contains the admin")
	require.Contains(t, members, newMemberDID, "contains the new member")
}

// GetMetadata supports two auth methods:
//  1. orgID in query params + an org-signed token in the Authorization header
//  2. a regular authenticated caller (no orgID), resolved to their org
func TestGetMetadataViaSignedToken(t *testing.T) {
	srv, orgId := newTestServer(t, did1, &fakeInstancePolicy{policy: "open"})

	// Mint an org-signed token to authenticate the request.
	org, err := srv.store.GetOrg(context.Background(), orgId)
	require.NoError(t, err)
	token, err := org.IssueIdentityToken(
		context.Background(),
		did1,
		true,
		time.Now().Add(time.Hour),
	)
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.org.getMetadata?OrgId="+orgId.String(),
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	srv.GetMetadata(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatOrgGetMetadataOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.Equal(t, orgId.String(), out.OrgId)
	require.Equal(t, "test-org", out.Name)
	require.Equal(t, string(LoginMethodPassword), out.LoginMethod)
}

func TestGetMetadataViaSignedToken_InvalidToken(t *testing.T) {
	srv, orgId := newTestServer(t, did1, &fakeInstancePolicy{policy: "open"})

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.org.getMetadata?OrgId="+orgId.String(),
		nil,
	)
	req.Header.Set("Authorization", "Bearer not-a-valid-token")
	w := httptest.NewRecorder()
	srv.GetMetadata(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetMetadataViaAuthenticatedCaller(t *testing.T) {
	srv, orgId := newTestServer(t, did1, &fakeInstancePolicy{policy: "open"})

	// No orgID query param: the caller is resolved to their org via the
	// stub authn method configured in newTestServer (did1).
	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.org.getMetadata",
		nil,
	)
	w := httptest.NewRecorder()
	srv.GetMetadata(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatOrgGetMetadataOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.Equal(t, orgId.String(), out.OrgId)
	require.Equal(t, "test-org", out.Name)
	require.Equal(t, string(LoginMethodPassword), out.LoginMethod)
}

func newCreateTestServer(t *testing.T) *Server {
	t.Helper()
	srv, err := NewServer(
		newTestStore(t),
		nil,
		nil,
		"domain",
		identity.DefaultDirectory(),
		&fakeInstancePolicy{policy: "open"},
	)
	require.NoError(t, err)
	return srv
}

func TestCreateOrg(t *testing.T) {
	srv := newCreateTestServer(t)

	body, _ := json.Marshal(habitat.NetworkHabitatOrgCreateInput{
		Name:            "My Org",
		AdminHandle:     "admin",
		AdminPassword:   "securepassword123",
		LoginMethod:     "password",
		HandleSubdomain: "org",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.org.create",
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.CreateOrg(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatOrgCreateOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.NotEmpty(t, out.OrgId)
	require.NotEmpty(t, out.AdminDid)
	require.Contains(t, out.AdminHandle, "admin")
	require.Equal(t, "My Org", out.Name)

	adminDID, err := syntax.ParseDID(out.AdminDid)
	require.NoError(t, err)

	orgID, err := syntax.ParseDID(out.OrgId)
	require.NoError(t, err)

	org, err := srv.store.GetOrg(context.Background(), orgID)
	require.NoError(t, err)
	members, err := org.GetMembers(context.Background())
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.Equal(t, adminDID, members[0])

	admins, err := org.GetAdmins(context.Background())
	require.NoError(t, err)
	require.Len(t, admins, 1)
	require.Equal(t, adminDID, admins[0])
}

func TestCreateOrg_InvalidHandle(t *testing.T) {
	srv := newCreateTestServer(t)

	body, _ := json.Marshal(habitat.NetworkHabitatOrgCreateInput{
		AdminHandle:     "invalid handle with spaces!",
		AdminPassword:   "password",
		LoginMethod:     "password",
		HandleSubdomain: "org",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.org.create",
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.CreateOrg(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateOrg_MissingFields(t *testing.T) {
	srv := newCreateTestServer(t)

	body, _ := json.Marshal(habitat.NetworkHabitatOrgCreateInput{
		AdminHandle:     "admin",
		HandleSubdomain: "org",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.org.create",
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.CreateOrg(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateOrg_OpenPolicyIgnoresMissingToken(t *testing.T) {
	srv, err := NewServer(
		newTestStore(t),
		authn.NewStubAuthnForTest(did1),
		nil,
		"pear.example.com",
		identity.DefaultDirectory(),
		&fakeInstancePolicy{policy: "open"},
	)
	require.NoError(t, err)

	body, _ := json.Marshal(habitat.NetworkHabitatOrgCreateInput{
		Name:            "test-org",
		AdminHandle:     "admin",
		AdminPassword:   "password",
		LoginMethod:     "password",
		HandleSubdomain: "test",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.org.create",
		bytes.NewReader(body),
	)
	rec := httptest.NewRecorder()
	srv.CreateOrg(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestCreateOrg_InviteOnlyRejectsMissingToken(t *testing.T) {
	srv, err := NewServer(
		newTestStore(t),
		authn.NewStubAuthnForTest(did1),
		nil,
		"pear.example.com",
		identity.DefaultDirectory(),
		&fakeInstancePolicy{policy: "invite_only"},
	)
	require.NoError(t, err)

	body, _ := json.Marshal(habitat.NetworkHabitatOrgCreateInput{
		Name:            "test-org",
		AdminHandle:     "admin",
		AdminPassword:   "password",
		LoginMethod:     "password",
		HandleSubdomain: "test",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.org.create",
		bytes.NewReader(body),
	)
	rec := httptest.NewRecorder()
	srv.CreateOrg(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCreateOrg_InviteOnlyRejectsInvalidToken(t *testing.T) {
	srv, err := NewServer(
		newTestStore(t),
		authn.NewStubAuthnForTest(did1),
		nil,
		"pear.example.com",
		identity.DefaultDirectory(),
		&fakeInstancePolicy{policy: "invite_only", validateErr: errors.New("bad token")},
	)
	require.NoError(t, err)

	body, _ := json.Marshal(habitat.NetworkHabitatOrgCreateInput{
		Name:            "test-org",
		AdminHandle:     "admin",
		AdminPassword:   "password",
		LoginMethod:     "password",
		HandleSubdomain: "test",
		InviteToken:     "garbage",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.org.create",
		bytes.NewReader(body),
	)
	rec := httptest.NewRecorder()
	srv.CreateOrg(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCreateOrg_InviteOnlyAcceptsValidToken(t *testing.T) {
	policy := &fakeInstancePolicy{policy: "invite_only"}
	srv, err := NewServer(
		newTestStore(t),
		authn.NewStubAuthnForTest(did1),
		nil,
		"pear.example.com",
		identity.DefaultDirectory(),
		policy,
	)
	require.NoError(t, err)

	body, _ := json.Marshal(habitat.NetworkHabitatOrgCreateInput{
		Name:            "test-org",
		AdminHandle:     "admin",
		AdminPassword:   "password",
		LoginMethod:     "password",
		HandleSubdomain: "test",
		InviteToken:     "a-valid-token",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.org.create",
		bytes.NewReader(body),
	)
	rec := httptest.NewRecorder()
	srv.CreateOrg(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []string{"a-valid-token"}, policy.validatedTokens)
	require.Equal(t, []string{"a-valid-token"}, policy.markedUsedTokens)
}

func TestCreateOrg_InviteOnlyDoesNotMarkUsedOnCreateFailure(t *testing.T) {
	policy := &fakeInstancePolicy{policy: "invite_only"}
	store := newTestStore(t)
	srv, err := NewServer(
		store,
		authn.NewStubAuthnForTest(did1),
		nil,
		"pear.example.com",
		identity.DefaultDirectory(),
		policy,
	)
	require.NoError(t, err)

	// Pre-create an org using the same handle subdomain so the subsequent
	// CreateOrg call fails (the hive identity mint for that subdomain
	// collides, surfacing as a generic creation failure - the precise
	// status code isn't the point here, only that CreateOrg fails and the
	// invite must not be marked used in that case).
	_, _, err = store.CreateOrg(
		t.Context(),
		"existing-org",
		"admin",
		"password",
		"password",
		"",
		"test",
	)
	require.NoError(t, err)

	body, _ := json.Marshal(habitat.NetworkHabitatOrgCreateInput{
		Name:            "test-org",
		AdminHandle:     "admin",
		AdminPassword:   "password",
		LoginMethod:     "password",
		HandleSubdomain: "test",
		InviteToken:     "a-valid-token",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.org.create",
		bytes.NewReader(body),
	)
	rec := httptest.NewRecorder()
	srv.CreateOrg(rec, req)

	require.NotEqual(t, http.StatusOK, rec.Code)
	require.Equal(t, []string{"a-valid-token"}, policy.validatedTokens)
	require.Empty(t, policy.markedUsedTokens)
}

// TestCreateOrg_InviteOnlyAcceptsRealIssuedToken proves a real
// instanceadmin.Store-issued invite is accepted end-to-end by a real
// org.Server.CreateOrg, not just by the fakeInstancePolicy used elsewhere in
// this file.
func TestCreateOrg_InviteOnlyAcceptsRealIssuedToken(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	instanceStore, err := instance.NewStore(db, []byte("key"), "passhash", "pear.example.com")
	require.NoError(t, err)
	require.NoError(t, instanceStore.UpdateSettings(t.Context(), "Acme Hosting", "invite_only"))
	token, err := instanceStore.IssueInvite(t.Context())
	require.NoError(t, err)

	srv, err := NewServer(
		newTestStore(t),
		authn.NewStubAuthnForTest(did1),
		nil,
		"pear.example.com",
		identity.DefaultDirectory(),
		instanceStore,
	)
	require.NoError(t, err)

	body, _ := json.Marshal(habitat.NetworkHabitatOrgCreateInput{
		Name:            "test-org",
		AdminHandle:     "admin",
		AdminPassword:   "password",
		LoginMethod:     "password",
		HandleSubdomain: "test",
		InviteToken:     token,
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.org.create",
		bytes.NewReader(body),
	)
	rec := httptest.NewRecorder()
	srv.CreateOrg(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	// The real invite should now be consumed - validating it again should fail.
	require.ErrorIs(
		t,
		instanceStore.ValidateInvite(t.Context(), token),
		instance.ErrInvalidInvite,
	)
}
