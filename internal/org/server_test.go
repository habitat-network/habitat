package org

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/stretchr/testify/require"
)

func newTestServer(t *testing.T, adminDID syntax.DID) (*Server, syntax.DID) {
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

	srv, err := NewServer(storeImpl, authn.NewStubAuthnForTest(adminDID), nil, "pear.example.com", identity.DefaultDirectory())
	require.NoError(t, err)
	return srv, orgIdIdent.DID
}

func TestIssueTokenThenMintIdentity(t *testing.T) {
	srv, orgId := newTestServer(t, did1)

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
	srv, orgId := newTestServer(t, did1)

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
		"/xrpc/network.habitat.org.getMetadata?OrgId="+orgId,
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	srv.GetMetadata(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatOrgGetMetadataOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.Equal(t, orgId, out.OrgId)
	require.Equal(t, "test-org", out.Name)
	require.Equal(t, string(LoginMethodPassword), out.LoginMethod)
}

func TestGetMetadataViaSignedToken_InvalidToken(t *testing.T) {
	srv, orgId := newTestServer(t, did1)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.org.getMetadata?OrgId="+orgId,
		nil,
	)
	req.Header.Set("Authorization", "Bearer not-a-valid-token")
	w := httptest.NewRecorder()
	srv.GetMetadata(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetMetadataViaAuthenticatedCaller(t *testing.T) {
	srv, orgId := newTestServer(t, did1)

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
	require.Equal(t, orgId, out.OrgId)
	require.Equal(t, "test-org", out.Name)
	require.Equal(t, string(LoginMethodPassword), out.LoginMethod)
}

func newCreateTestServer(t *testing.T) *Server {
	t.Helper()
	srv, err := NewServer(newTestStore(t), nil, nil, "domain", identity.DefaultDirectory())
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
