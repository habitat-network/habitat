package org

import (
	"context"
	"testing"
	"time"

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
		"",
		encrypt.TestKey,
		pdsclient.NewDummyDirectory("https://pds.example.com"),
	)
	s := &orgImpl{
		orgID:            "test-org",
		hive:             h,
		db:               db,
		signingSecret:    testSigningSecret,
		passwordProvider: passwordProvider,
	}
	require.NoError(t, err)
	s.handleSubdomain = "testorg"
	s.method = LoginMethodPassword
	s.name = "Test Org"
	return s
}

var (
	did1 = syntax.DID("did:plc:alice111")
	did2 = syntax.DID("did:plc:bob2222")
)

const (
	testPasswordHash = "testhash"
	testPassword     = "test-password-123"
)

func TestIsMember(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	ok, err := s.IsMember(ctx, did1)
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, s.addMemberTx(ctx, s.db, did1))

	ok, err = s.IsMember(ctx, did1)

	require.NoError(t, err)
	require.True(t, ok)
}

func TestAddAdmin_GetAdmins(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	require.NoError(t, s.addMemberTx(ctx, s.db, did1))
	require.NoError(t, s.AddAdmin(ctx, did1))

	admins, err := s.GetAdmins(ctx)
	require.NoError(t, err)
	require.Equal(t, []syntax.DID{did1}, admins)
}

func TestAddAdmin_NotMember(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	err := s.AddAdmin(ctx, did1)
	require.ErrorIs(t, err, ErrNotMember)
}

func TestRemoveAdmin_LastAdmin(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	require.NoError(t, s.addMemberTx(ctx, s.db, did1))
	require.NoError(t, s.AddAdmin(ctx, did1))

	err := s.RemoveAdmin(ctx, did1)
	require.ErrorIs(t, err, ErrLastAdmin)
}

func TestRemoveAdmin_MultipleAdmins(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	require.NoError(t, s.addMemberTx(ctx, s.db, did1))
	require.NoError(t, s.addMemberTx(ctx, s.db, did2))
	require.NoError(t, s.AddAdmin(ctx, did1))
	require.NoError(t, s.AddAdmin(ctx, did2))

	require.NoError(t, s.RemoveAdmin(ctx, did2))

	admins, err := s.GetAdmins(ctx)
	require.NoError(t, err)
	require.Equal(t, []syntax.DID{did1}, admins)
}

func TestGetMembers(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	members, err := s.GetMembers(ctx)
	require.NoError(t, err)
	require.Empty(t, members)

	require.NoError(t, s.addMemberTx(ctx, s.db, did1))
	require.NoError(t, s.addMemberTx(ctx, s.db, did2))

	members, err = s.GetMembers(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []syntax.DID{did1, did2}, members)
}

func TestRemoveMembers(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	require.NoError(t, s.addMemberTx(ctx, s.db, did1))
	require.NoError(t, s.addMemberTx(ctx, s.db, did2))
	require.NoError(t, s.RemoveMembers(ctx, []syntax.DID{did2}))

	ok, err := s.IsMember(ctx, did1)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = s.IsMember(ctx, did2)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestGenerateAndUseIdentityToken(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	token, err := s.IssueIdentityToken(ctx, did1, false, time.Now().Add(time.Hour))
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

	token, err := s.IssueIdentityToken(ctx, did1, false, time.Now().Add(time.Hour))
	require.NoError(t, err)

	_, err = s.CreateNewMemberIdentity(ctx, token, "alice", testPasswordHash, "")
	require.NoError(t, err)
	_, err = s.CreateNewMemberIdentity(ctx, token, "bob", testPasswordHash, "")
	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestMintIdentity_DuplicateHandle(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	token1, err := s.IssueIdentityToken(ctx, did1, false, time.Now().Add(time.Hour))
	require.NoError(t, err)
	token2, err := s.IssueIdentityToken(ctx, did1, false, time.Now().Add(time.Hour))
	require.NoError(t, err)

	_, err = s.CreateNewMemberIdentity(ctx, token1, "alice", testPasswordHash, "")
	require.NoError(t, err)
	_, err = s.CreateNewMemberIdentity(ctx, token2, "alice", testPasswordHash, "")
	require.ErrorIs(t, err, hive.ErrNotCreated)
}

func TestIdentityToken_Reusable(t *testing.T) {
	ctx := context.Background()
	s := newTestOrg(t)

	token, err := s.IssueIdentityToken(ctx, did1, true, time.Now().Add(time.Hour))
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

	_, err := s.IssueIdentityToken(ctx, did1, false, time.Now().AddDate(0, 1, 1))
	require.ErrorIs(t, err, ErrInvalidTokenExpiry)
}
