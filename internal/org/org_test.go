package org

import (
	"context"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testSigningSecret = []byte("test-signing-secret-for-org-00000")

func newTestOrg(t *testing.T) *orgImpl {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&organization{}, &member{}, &spentToken{}))
	h, err := hive.NewHive("example.com", "pear.example.com", db)
	require.NoError(t, err)
	passwordProvider, err := login.NewPasswordProvider(
		db,
		"",
		encrypt.TestKey,
		pdsclient.NewDummyDirectory("https://pds.example.com"),
	)
	require.NoError(t, err)
	s := &orgImpl{
		orgID:            "test-org",
		hive:             h,
		db:               db,
		signingSecret:    testSigningSecret,
		passwordProvider: passwordProvider,
		handleSubdomain:  "testorg",
		method:           LoginMethodPassword,
		name:             "Test Org",
	}
	return s
}

var adminDID = syntax.DID("did:plc:alice111")

const (
	testPasswordHash = "testhash"
	testPassword     = "test-password-123"
)

func addMember(t *testing.T, s *orgImpl, handle string) *identity.Identity {
	t.Helper()
	token, err := s.IssueIdentityToken(
		t.Context(),
		adminDID,
		true,
		time.Now().Add(time.Hour),
	)
	require.NoError(t, err)
	id, err := s.CreateNewMemberIdentity(t.Context(), token, handle, testPasswordHash, "")
	require.NoError(t, err)
	return id
}

func TestIsMember(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	id := addMember(t, s, "alice")

	ok, err := s.IsMember(ctx, id.DID)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestCreateNewMemberIdentityPasswordLoginIDIsDID(t *testing.T) {
	// A password member authenticates with a password keyed to their minted DID,
	// so their stored LoginID must be that DID. If it isn't, login fails at code
	// exchange with a "login id mismatch".
	s := newTestOrg(t)

	id := addMember(t, s, "alice")

	var m member
	require.NoError(t, s.db.Where("did = ?", id.DID).First(&m).Error)
	require.Equal(t, id.DID.String(), m.LoginID)
}

func TestAddAdmin_GetAdmins(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	id := addMember(t, s, "alice")
	require.NoError(t, s.AddAdmin(ctx, id.DID))

	admins, err := s.GetAdmins(ctx)
	require.NoError(t, err)
	require.Equal(t, []syntax.DID{id.DID}, admins)
}

func TestAddAdmin_NotMember(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	err := s.AddAdmin(ctx, adminDID)
	require.ErrorIs(t, err, ErrNotMember)
}

func TestRemoveAdmin_LastAdmin(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	id := addMember(t, s, "alice")
	require.NoError(t, s.AddAdmin(ctx, id.DID))

	err := s.RemoveAdmin(ctx, id.DID)
	require.ErrorIs(t, err, ErrLastAdmin)
}

func TestRemoveAdmin_MultipleAdmins(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	id1 := addMember(t, s, "alice")
	id2 := addMember(t, s, "bob")
	require.NoError(t, s.AddAdmin(ctx, id1.DID))
	require.NoError(t, s.AddAdmin(ctx, id2.DID))

	require.NoError(t, s.RemoveAdmin(ctx, id2.DID))

	admins, err := s.GetAdmins(ctx)
	require.NoError(t, err)
	require.Equal(t, []syntax.DID{id1.DID}, admins)
}

func TestGetMembers(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	members, err := s.GetMembers(ctx)
	require.NoError(t, err)
	require.Empty(t, members)

	id1 := addMember(t, s, "alice")
	id2 := addMember(t, s, "bob")

	members, err = s.GetMembers(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []syntax.DID{id1.DID, id2.DID}, members)
}

func TestRemoveMembers(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	id1 := addMember(t, s, "alice")
	id2 := addMember(t, s, "bob")
	require.NoError(t, s.RemoveMembers(ctx, []syntax.DID{id2.DID}))

	ok, err := s.IsMember(ctx, id1.DID)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = s.IsMember(ctx, id2.DID)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestGenerateAndUseIdentityToken(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	token, err := s.IssueIdentityToken(ctx, adminDID, false, time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.NotEmpty(t, token)

	_, err = s.CreateNewMemberIdentity(ctx, token, "alice", testPasswordHash, "")
	require.NoError(t, err)

	members, err := s.GetMembers(ctx)
	require.NoError(t, err)
	require.Len(t, members, 1)
}

func TestIdentityToken_CannotReuse(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	token, err := s.IssueIdentityToken(ctx, adminDID, false, time.Now().Add(time.Hour))
	require.NoError(t, err)

	_, err = s.CreateNewMemberIdentity(ctx, token, "alice", testPasswordHash, "")
	require.NoError(t, err)
	_, err = s.CreateNewMemberIdentity(ctx, token, "bob", testPasswordHash, "")
	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestMintIdentity_DuplicateHandle(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	token1, err := s.IssueIdentityToken(ctx, adminDID, false, time.Now().Add(time.Hour))
	require.NoError(t, err)
	token2, err := s.IssueIdentityToken(ctx, adminDID, false, time.Now().Add(time.Hour))
	require.NoError(t, err)

	_, err = s.CreateNewMemberIdentity(ctx, token1, "alice", testPasswordHash, "")
	require.NoError(t, err)
	_, err = s.CreateNewMemberIdentity(ctx, token2, "alice", testPasswordHash, "")
	require.ErrorIs(t, err, hive.ErrNotCreated)
}

func TestIdentityToken_Reusable(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	token, err := s.IssueIdentityToken(ctx, adminDID, true, time.Now().Add(time.Hour))
	require.NoError(t, err)

	_, err = s.CreateNewMemberIdentity(ctx, token, "alice", testPasswordHash, "")
	require.NoError(t, err)
	_, err = s.CreateNewMemberIdentity(ctx, token, "bob", testPasswordHash, "")
	require.NoError(t, err)
	_, err = s.CreateNewMemberIdentity(ctx, token, "alice", testPasswordHash, "")
	require.ErrorIs(t, err, hive.ErrNotCreated)
}

func TestIssueIdentityToken_ExpiryTooLate(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	_, err := s.IssueIdentityToken(ctx, adminDID, false, time.Now().AddDate(0, 1, 1))
	require.ErrorIs(t, err, ErrInvalidTokenExpiry)
}
