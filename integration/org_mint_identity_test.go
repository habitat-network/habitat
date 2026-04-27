package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMintThenLookup(t *testing.T) {
	ctx := context.Background()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)

	h, err := hive.NewHive("example.com", "pear.example.com", db)
	require.NoError(t, err)

	adminDID := syntax.DID("did:plc:admin1234")
	o, err := org.NewOrg("example.com", h, db, []byte("test-signing-secret-for-org-00000"))
	require.NoError(t, err)
	require.NoError(t, o.AddAdmin(ctx, adminDID))

	orgServer, err := org.NewServer(o, authn.NewStubAuthnForTest(adminDID))
	require.NoError(t, err)

	hiveServer, err := hive.NewServer(h)
	require.NoError(t, err)

	// Admin issues an invite token via org server
	issueBody, _ := json.Marshal(habitat.NetworkHabitatOrgIssueInviteTokenInput{})
	issueReq := httptest.NewRequest(http.MethodPost, "/xrpc/network.habitat.org.issueInviteToken", bytes.NewReader(issueBody))
	issueReq.Header.Set("Content-Type", "application/json")
	issueW := httptest.NewRecorder()
	orgServer.IssueInviteToken(issueW, issueReq)
	require.Equal(t, http.StatusOK, issueW.Code)

	var issueOut habitat.NetworkHabitatOrgIssueInviteTokenOutput
	require.NoError(t, json.NewDecoder(issueW.Body).Decode(&issueOut))
	require.NotEmpty(t, issueOut.Token)

	// New member uses the token to mint an identity via org server
	mintBody, _ := json.Marshal(habitat.NetworkHabitatOrgMintMemberIdentityInput{
		Token:  issueOut.Token,
		Handle: "alice",
	})
	mintReq := httptest.NewRequest(http.MethodPost, "/xrpc/network.habitat.org.mintMemberIdentity", bytes.NewReader(mintBody))
	mintReq.Header.Set("Content-Type", "application/json")
	mintW := httptest.NewRecorder()
	orgServer.MintMemberIdentity(mintW, mintReq)
	require.Equal(t, http.StatusOK, mintW.Code)

	var mintOut habitat.NetworkHabitatOrgMintMemberIdentityOutput
	require.NoError(t, json.NewDecoder(mintW.Body).Decode(&mintOut))
	require.True(t, strings.HasPrefix(mintOut.Did, "did:web:"))

	// Verify via hive server: resolve handle -> DID
	handleReq := httptest.NewRequest(http.MethodGet, "/.well-known/atproto-did", nil)
	handleReq.Host = "alice.example.com"
	handleW := httptest.NewRecorder()
	hiveServer.ServeHandle(handleW, handleReq)
	require.Equal(t, http.StatusOK, handleW.Code)
	did := strings.TrimSpace(handleW.Body.String())
	require.Equal(t, mintOut.Did, did)

	// Verify via hive server: resolve DID -> DID doc
	didHost := strings.TrimPrefix(did, "did:web:")
	docReq := httptest.NewRequest(http.MethodGet, "/.well-known/did.json", nil)
	docReq.Host = didHost
	docW := httptest.NewRecorder()
	hiveServer.ServeDIDDoc(docW, docReq)
	require.Equal(t, http.StatusOK, docW.Code)
	require.Equal(t, "application/did+ld+json", docW.Header().Get("Content-Type"))

	var doc map[string]any
	require.NoError(t, json.Unmarshal(docW.Body.Bytes(), &doc))
	require.Equal(t, did, doc["id"])
	akaSlice, ok := doc["alsoKnownAs"].([]interface{})
	require.True(t, ok)
	require.Len(t, akaSlice, 1)
	require.Equal(t, "at://alice.example.com", akaSlice[0])
}
