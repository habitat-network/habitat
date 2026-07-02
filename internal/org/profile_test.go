package org_test

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
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/events"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/instance"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const profileCollection = syntax.NSID("app.bsky.actor.profile")

// This lives in an external (_test) package, rather than alongside the rest
// of internal/org's tests, because internal/spaces depends on internal/org
// for its HTTP layer: an internal test file importing internal/spaces would
// form an import cycle.

type fakeInstancePolicy struct {
	policy instance.InvitePolicy
}

func (f *fakeInstancePolicy) GetOrgCreationPolicy(context.Context) (instance.InvitePolicy, error) {
	return f.policy, nil
}

func (f *fakeInstancePolicy) ValidateInvite(context.Context, string) error { return nil }

func (f *fakeInstancePolicy) MarkInviteUsed(context.Context, string) error { return nil }

func newTestOrgStore(t *testing.T) org.Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	h, err := hive.NewHive("example.com", "pear.example.com", db)
	require.NoError(t, err)
	passwordProvider, err := login.NewPasswordProvider(
		db,
		"pear.example.com",
		[]byte("test-signing-secret-for-org-00000"),
		pdsclient.NewDummyDirectory("https://pds.example.com"),
	)
	require.NoError(t, err)
	store, err := org.NewStore(
		db,
		h,
		pdsclient.NewDummyDirectory("https://pds.example.com"),
		"pear.example.com",
		passwordProvider,
	)
	require.NoError(t, err)
	return store
}

// newTestSpacesStore creates a standalone spaces.Store backed by its own
// in-memory sqlite DB and FGA instance, for verifying profile records
// written by org.Server.
func newTestSpacesStore(t *testing.T) spaces.Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })
	eventStore, err := events.NewStore(db)
	require.NoError(t, err)
	sp, err := spaces.NewStore(db, fga, eventStore)
	require.NoError(t, err)
	return sp
}

func TestCreateOrg_WritesProfileRecord(t *testing.T) {
	sp := newTestSpacesStore(t)
	srv, err := org.NewServer(
		newTestOrgStore(t),
		nil,
		sp,
		"domain",
		identity.DefaultDirectory(),
		&fakeInstancePolicy{policy: "open"},
	)
	require.NoError(t, err)

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

	orgID, err := syntax.ParseDID(out.OrgId)
	require.NoError(t, err)
	adminDID, err := syntax.ParseDID(out.AdminDid)
	require.NoError(t, err)

	spaceURI := habitat_syntax.ConstructSpaceURI(
		orgID,
		habitat_syntax.ProfilesSpaceType,
		habitat_syntax.ProfilesSpaceKey,
	)
	rec, err := sp.GetRecord(
		t.Context(),
		spaceURI,
		adminDID,
		profileCollection,
		syntax.RecordKey("self"),
	)
	require.NoError(t, err)
	require.Equal(t, "app.bsky.actor.profile", rec.Value["$type"])
	require.Equal(t, out.AdminHandle, rec.Value["displayName"])
}

func TestMintMemberIdentity_WritesProfileRecord(t *testing.T) {
	sp := newTestSpacesStore(t)
	storeImpl := newTestOrgStore(t)
	orgIdIdent, adminIdent, err := storeImpl.CreateOrg(
		t.Context(),
		"test-org",
		"admin",
		"password",
		"password",
		"",
		"",
	)
	require.NoError(t, err)

	srv, err := org.NewServer(
		storeImpl,
		authn.NewStubAuthnForTest(adminIdent.DID),
		sp,
		"pear.example.com",
		identity.DefaultDirectory(),
		&fakeInstancePolicy{policy: "open"},
	)
	require.NoError(t, err)

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

	mintBody, _ := json.Marshal(habitat.NetworkHabitatOrgMintMemberIdentityInput{
		OrgId:    orgIdIdent.DID.String(),
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

	memberDID, err := syntax.ParseDID(mintOut.Did)
	require.NoError(t, err)

	spaceURI := habitat_syntax.ConstructSpaceURI(
		orgIdIdent.DID,
		habitat_syntax.ProfilesSpaceType,
		habitat_syntax.ProfilesSpaceKey,
	)
	rec, err := sp.GetRecord(
		t.Context(),
		spaceURI,
		memberDID,
		profileCollection,
		syntax.RecordKey("self"),
	)
	require.NoError(t, err)
	require.Equal(t, "app.bsky.actor.profile", rec.Value["$type"])
	require.Equal(t, mintOut.Handle, rec.Value["displayName"])
}
