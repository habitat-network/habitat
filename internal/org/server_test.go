package org

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/stretchr/testify/require"
)

func newTestServer(t *testing.T) (*Server, syntax.DID) {
	t.Helper()
	s := newTestStoreWithHive(t)
	ident, opaqueID, persist, err := s.hive.MintIdentity("admin")
	require.NoError(t, err)
	require.NoError(t, persist(s.db))
	require.NoError(t, s.addMember(context.Background(), ID(opaqueID), testPasswordHash))
	require.NoError(t, s.AddAdmin(context.Background(), ident.DID))
	srv, err := NewServer(s, authn.NewStubAuthnForTest(ident.DID))
	require.NoError(t, err)
	return srv, ident.DID
}

func TestIssueTokenThenMintIdentity(t *testing.T) {
	srv, adminDID := newTestServer(t)

	// Admin issues an invite token
	issueBody, _ := json.Marshal(habitat.NetworkHabitatOrgIssueInviteTokenInput{
		ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339),
	})
	issueReq := httptest.NewRequest(http.MethodPost, "/xrpc/network.habitat.org.issueInviteToken", bytes.NewReader(issueBody))
	issueReq.Header.Set("Content-Type", "application/json")
	issueW := httptest.NewRecorder()
	srv.IssueInviteToken(issueW, issueReq)
	require.Equal(t, http.StatusOK, issueW.Code)

	var issueOut habitat.NetworkHabitatOrgIssueInviteTokenOutput
	require.NoError(t, json.NewDecoder(issueW.Body).Decode(&issueOut))
	require.NotEmpty(t, issueOut.Token)

	// Someone uses the token to mint an identity
	mintBody, _ := json.Marshal(habitat.NetworkHabitatOrgMintMemberIdentityInput{
		Token:    issueOut.Token,
		Password: "password",
		Handle:   "alice",
	})
	mintReq := httptest.NewRequest(http.MethodPost, "/xrpc/network.habitat.org.mintMemberIdentity", bytes.NewReader(mintBody))
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

	members, err := srv.org.GetMembers(context.Background())
	require.NoError(t, err)
	require.Len(t, members, 2)
	require.Contains(t, members, adminDID, "contains the admin")
	require.Contains(t, members, newMemberDID, "contains the new member")
}
