package org

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestServer(t *testing.T, adminDID syntax.DID) *Server {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	h, err := hive.NewHive("example.com", "pear.example.com", db)
	require.NoError(t, err)
	storeImpl, err := NewStore(db, h, identity.DefaultDirectory(), "pear.example.com")
	require.NoError(t, err)

	require.NoError(t, db.Create(&Organization{
		ID:            "test-org",
		Domain:        "example.com",
		SigningSecret: base64.StdEncoding.EncodeToString(testSigningSecret),
	}).Error)

	scoped, err := storeImpl.GetOrg(context.Background(), "test-org")
	require.NoError(t, err)
	st := scoped.(*store)
	require.NoError(t, st.addMember(context.Background(), adminDID, testPasswordHash))
	require.NoError(t, st.AddAdmin(context.Background(), adminDID))

	srv, err := NewServer(storeImpl, authn.NewStubAuthnForTest(adminDID))
	require.NoError(t, err)
	return srv
}

func TestIssueTokenThenMintIdentity(t *testing.T) {
	srv := newTestServer(t, did1)

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
		OrgId:    "test-org",
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

	testOrg, err := srv.store.GetOrg(context.Background(), "test-org")
	require.NoError(t, err)
	members, err := testOrg.GetMembers(context.Background())
	require.NoError(t, err)
	require.Len(t, members, 2)
	require.Contains(t, members, did1, "contains the admin")
	require.Contains(t, members, newMemberDID, "contains the new member")
}
