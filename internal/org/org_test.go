package org

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/core"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testSigningSecret = []byte("test-signing-secret-for-org-00000")

func newTestOrg(t *testing.T) (*storeImpl, *orgImpl) {
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

	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)

	st, err := NewStore(
		db,
		h,
		pdsclient.NewDummyDirectory("https://pds.example.com"),
		"pear.example.com",
		passwordProvider,
		fga,
	)
	require.NoError(t, err)
	store := st.(*storeImpl)

	orgDid := syntax.DID("test-org")
	signingSecret := base64.StdEncoding.EncodeToString(testSigningSecret)
	require.NoError(t, store.db.Create(&organization{
		ID:              orgDid,
		Name:            "Test Org",
		LoginMethod:     core.LoginMethodPassword,
		SigningSecret:   signingSecret,
		HandleSubdomain: "testorg",
	}).Error)

	org, err := store.GetOrg(t.Context(), orgDid)
	require.NoError(t, err)
	orgImpl := org.(*orgImpl)

	return store, orgImpl
}

var adminDID = syntax.DID("did:plc:alice111")

const (
	testPasswordHash = "testhash"
	testPassword     = "test-password-123"
)

func addMember(t *testing.T, store *storeImpl, org *orgImpl, handle string) *identity.Identity {
	t.Helper()
	token, err := store.IssueIdentityToken(
		t.Context(),
		org.orgID,
		adminDID,
		true,
		time.Now().Add(time.Hour),
	)
	require.NoError(t, err)
	id, err := store.CreateNewMemberIdentity(
		t.Context(),
		org.orgID,
		token,
		handle,
		testPasswordHash,
		"",
	)
	require.NoError(t, err)
	return id
}

func TestIsMember(t *testing.T) {
	ctx := context.Background()
	store, org := newTestOrg(t)

	id := addMember(t, store, org, "alice")

	ok, err := org.IsMember(ctx, id.DID)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestCreateNewMemberIdentityPasswordLoginIDIsDID(t *testing.T) {
	store, org := newTestOrg(t)

	id := addMember(t, store, org, "alice")

	var m member
	require.NoError(t, org.db.Where("did = ?", id.DID).First(&m).Error)
	require.Equal(t, id.DID.String(), m.LoginID)
}

func TestAddAdmin_GetAdmins(t *testing.T) {
	ctx := context.Background()
	store, org := newTestOrg(t)

	id := addMember(t, store, org, "alice")
	require.NoError(t, org.AddAdmin(ctx, id.DID))

	admins, err := org.GetAdmins(ctx)
	require.NoError(t, err)
	require.Equal(t, []syntax.DID{id.DID}, admins)
}

func TestAddAdmin_NotMember(t *testing.T) {
	ctx := context.Background()
	_, org := newTestOrg(t)

	err := org.AddAdmin(ctx, adminDID)
	require.ErrorIs(t, err, ErrNotMember)
}

func TestRemoveAdmin_LastAdmin(t *testing.T) {
	ctx := context.Background()
	store, org := newTestOrg(t)

	id := addMember(t, store, org, "alice")
	require.NoError(t, org.AddAdmin(ctx, id.DID))

	err := org.RemoveAdmin(ctx, id.DID)
	require.ErrorIs(t, err, ErrLastAdmin)
}

func TestRemoveAdmin_MultipleAdmins(t *testing.T) {
	ctx := context.Background()
	store, org := newTestOrg(t)

	id1 := addMember(t, store, org, "alice")
	id2 := addMember(t, store, org, "bob")
	require.NoError(t, org.AddAdmin(ctx, id1.DID))
	require.NoError(t, org.AddAdmin(ctx, id2.DID))

	require.NoError(t, org.RemoveAdmin(ctx, id2.DID))

	admins, err := org.GetAdmins(ctx)
	require.NoError(t, err)
	require.Equal(t, []syntax.DID{id1.DID}, admins)
}

func TestGetMembers(t *testing.T) {
	ctx := context.Background()
	store, org := newTestOrg(t)

	members, err := org.GetMembers(ctx)
	require.NoError(t, err)
	require.Empty(t, members)

	id1 := addMember(t, store, org, "alice")
	id2 := addMember(t, store, org, "bob")

	members, err = org.GetMembers(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []syntax.DID{id1.DID, id2.DID}, members)
}

func TestRemoveMembers(t *testing.T) {
	ctx := context.Background()
	store, org := newTestOrg(t)

	id1 := addMember(t, store, org, "alice")
	id2 := addMember(t, store, org, "bob")
	require.NoError(t, org.RemoveMembers(ctx, []syntax.DID{id2.DID}))

	ok, err := org.IsMember(ctx, id1.DID)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = org.IsMember(ctx, id2.DID)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestAddAdmin_RemovesMemberFGA(t *testing.T) {
	ctx := context.Background()
	store, org := newTestOrg(t)

	id := addMember(t, store, org, "alice")
	memberUser := fgastore.MemberUserString(id.DID)
	orgObj := fgastore.OrgObjectKey(org.orgID)

	// Before promotion: has member tuple, no admin tuple
	tuples, err := org.fga.Read(ctx, fgastore.Tuple{
		User:     memberUser,
		Relation: fgastore.RelationMember,
		Object:   orgObj,
	})
	require.NoError(t, err)
	require.Len(t, tuples, 1)

	tuples, err = org.fga.Read(ctx, fgastore.Tuple{
		User:     memberUser,
		Relation: fgastore.RelationAdmin,
		Object:   orgObj,
	})
	require.NoError(t, err)
	require.Empty(t, tuples)

	require.NoError(t, org.AddAdmin(ctx, id.DID))

	// After promotion: has admin tuple, no member tuple
	tuples, err = org.fga.Read(ctx, fgastore.Tuple{
		User:     memberUser,
		Relation: fgastore.RelationAdmin,
		Object:   orgObj,
	})
	require.NoError(t, err)
	require.Len(t, tuples, 1)

	tuples, err = org.fga.Read(ctx, fgastore.Tuple{
		User:     memberUser,
		Relation: fgastore.RelationMember,
		Object:   orgObj,
	})
	require.NoError(t, err)
	require.Empty(t, tuples)
}

func TestDowngradeAdmin(t *testing.T) {
	ctx := context.Background()
	store, org := newTestOrg(t)

	id1 := addMember(t, store, org, "alice")
	id2 := addMember(t, store, org, "bob")
	require.NoError(t, org.AddAdmin(ctx, id1.DID))
	require.NoError(t, org.AddAdmin(ctx, id2.DID))

	memberUser := fgastore.MemberUserString(id1.DID)
	orgObj := fgastore.OrgObjectKey(org.orgID)

	require.NoError(t, org.DowngradeAdmin(ctx, id1.DID))

	// After downgrade: has member tuple, no admin tuple
	tuples, err := org.fga.Read(ctx, fgastore.Tuple{
		User:     memberUser,
		Relation: fgastore.RelationMember,
		Object:   orgObj,
	})
	require.NoError(t, err)
	require.Len(t, tuples, 1)

	tuples, err = org.fga.Read(ctx, fgastore.Tuple{
		User:     memberUser,
		Relation: fgastore.RelationAdmin,
		Object:   orgObj,
	})
	require.NoError(t, err)
	require.Empty(t, tuples)

	// DB-level verification
	ok, err := org.IsMember(ctx, id1.DID)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = org.IsAdmin(ctx, id1.DID)
	require.NoError(t, err)
	require.False(t, ok)

	admins, err := org.GetAdmins(ctx)
	require.NoError(t, err)
	require.Equal(t, []syntax.DID{id2.DID}, admins)
}

func TestDowngradeAdmin_LastAdmin(t *testing.T) {
	ctx := context.Background()
	store, org := newTestOrg(t)

	id := addMember(t, store, org, "alice")
	require.NoError(t, org.AddAdmin(ctx, id.DID))

	err := org.DowngradeAdmin(ctx, id.DID)
	require.ErrorIs(t, err, ErrLastAdmin)

	// Still an admin
	ok, err := org.IsAdmin(ctx, id.DID)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestRemoveMembers_RemovesFGATuples(t *testing.T) {
	ctx := context.Background()
	store, org := newTestOrg(t)

	id := addMember(t, store, org, "alice")
	memberUser := fgastore.MemberUserString(id.DID)
	orgObj := fgastore.OrgObjectKey(org.orgID)

	require.NoError(t, org.RemoveMembers(ctx, []syntax.DID{id.DID}))

	// FGA member tuple should be gone
	tuples, err := org.fga.Read(ctx, fgastore.Tuple{
		User:     memberUser,
		Relation: fgastore.RelationMember,
		Object:   orgObj,
	})
	require.NoError(t, err)
	require.Empty(t, tuples)
}

func TestGenerateAndUseIdentityToken(t *testing.T) {
	ctx := context.Background()
	store, org := newTestOrg(t)

	token, err := store.IssueIdentityToken(
		ctx,
		org.orgID,
		adminDID,
		false,
		time.Now().Add(time.Hour),
	)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	_, err = store.CreateNewMemberIdentity(ctx, org.orgID, token, "alice", testPasswordHash, "")
	require.NoError(t, err)

	members, err := org.GetMembers(ctx)
	require.NoError(t, err)
	require.Len(t, members, 1)
}

func TestIdentityToken_CannotReuse(t *testing.T) {
	ctx := context.Background()
	store, org := newTestOrg(t)

	token, err := store.IssueIdentityToken(
		ctx,
		org.orgID,
		adminDID,
		false,
		time.Now().Add(time.Hour),
	)
	require.NoError(t, err)

	_, err = store.CreateNewMemberIdentity(ctx, org.orgID, token, "alice", testPasswordHash, "")
	require.NoError(t, err)
	_, err = store.CreateNewMemberIdentity(ctx, org.orgID, token, "bob", testPasswordHash, "")
	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestMintIdentity_DuplicateHandle(t *testing.T) {
	ctx := context.Background()
	store, org := newTestOrg(t)

	token1, err := store.IssueIdentityToken(
		ctx,
		org.orgID,
		adminDID,
		false,
		time.Now().Add(time.Hour),
	)
	require.NoError(t, err)
	token2, err := store.IssueIdentityToken(
		ctx,
		org.orgID,
		adminDID,
		false,
		time.Now().Add(time.Hour),
	)
	require.NoError(t, err)

	_, err = store.CreateNewMemberIdentity(ctx, org.orgID, token1, "alice", testPasswordHash, "")
	require.NoError(t, err)
	_, err = store.CreateNewMemberIdentity(ctx, org.orgID, token2, "alice", testPasswordHash, "")
	require.ErrorIs(t, err, hive.ErrNotCreated)
}

func TestIdentityToken_Reusable(t *testing.T) {
	ctx := context.Background()
	store, org := newTestOrg(t)

	token, err := store.IssueIdentityToken(
		ctx,
		org.orgID,
		adminDID,
		true,
		time.Now().Add(time.Hour),
	)
	require.NoError(t, err)

	_, err = store.CreateNewMemberIdentity(ctx, org.orgID, token, "alice", testPasswordHash, "")
	require.NoError(t, err)
	_, err = store.CreateNewMemberIdentity(ctx, org.orgID, token, "bob", testPasswordHash, "")
	require.NoError(t, err)
	_, err = store.CreateNewMemberIdentity(ctx, org.orgID, token, "alice", testPasswordHash, "")
	require.ErrorIs(t, err, hive.ErrNotCreated)
}

func TestIssueIdentityToken_ExpiryTooLate(t *testing.T) {
	ctx := context.Background()
	store, org := newTestOrg(t)

	_, err := store.IssueIdentityToken(ctx, org.orgID, adminDID, false, time.Now().AddDate(0, 1, 1))
	require.ErrorIs(t, err, ErrInvalidTokenExpiry)
}
