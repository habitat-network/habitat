package org_test

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
)

var testSigningSecret = []byte("test-signing-secret-for-org-00000")

func newTestOrg(t *testing.T) (*org.StoreImpl, *org.OrgImpl) {
	t.Helper()
	db := testutil.NewPearDB(t)
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

	st, err := org.NewStore(
		db,
		h,
		pdsclient.NewDummyDirectory("https://pds.example.com"),
		"pear.example.com",
		passwordProvider,
		fga,
		org.NewEveryoneOrg("pear.example.com"),
	)
	require.NoError(t, err)
	store := st.(*org.StoreImpl)

	orgDid := syntax.DID("test-org")
	signingSecret := base64.StdEncoding.EncodeToString(testSigningSecret)
	require.NoError(t, store.DB().Create(&org.Organization{
		ID:              orgDid,
		Name:            "Test Org",
		LoginMethod:     org.LoginMethodPassword,
		SigningSecret:   signingSecret,
		HandleSubdomain: "testorg",
	}).Error)

	orgHandle, err := store.GetOrg(t.Context(), orgDid)
	require.NoError(t, err)
	orgImpl := orgHandle.(*org.OrgImpl)

	return store, orgImpl
}

var adminDID = syntax.DID("did:plc:alice111")

const (
	testPasswordHash = "testhash"
	testPassword     = "test-password-123"
)

func addMember(
	t *testing.T,
	store *org.StoreImpl,
	orgHandle *org.OrgImpl,
	handle string,
) *identity.Identity {
	t.Helper()
	token, err := store.IssueIdentityToken(
		t.Context(),
		orgHandle.OrgID(),
		adminDID,
		true,
		time.Now().Add(time.Hour),
	)
	require.NoError(t, err)
	id, err := store.CreateNewMemberIdentity(
		t.Context(),
		orgHandle.OrgID(),
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
	store, orgHandle := newTestOrg(t)

	id := addMember(t, store, orgHandle, "alice")

	ok, err := orgHandle.IsMember(ctx, id.DID)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestCreateNewMemberIdentityPasswordLoginIDIsDID(t *testing.T) {
	store, orgHandle := newTestOrg(t)

	id := addMember(t, store, orgHandle, "alice")

	var m org.MemberRow
	require.NoError(t, orgHandle.OrgDB().Where("did = ?", id.DID).First(&m).Error)
	require.Equal(t, id.DID.String(), m.LoginID)
}

func TestAddAdmin_GetAdmins(t *testing.T) {
	ctx := context.Background()
	store, orgHandle := newTestOrg(t)

	id := addMember(t, store, orgHandle, "alice")
	require.NoError(t, orgHandle.AddAdmin(ctx, id.DID))

	admins, err := orgHandle.GetAdmins(ctx)
	require.NoError(t, err)
	require.Equal(t, []syntax.DID{id.DID}, admins)
}

func TestAddAdmin_NotMember(t *testing.T) {
	ctx := context.Background()
	_, orgHandle := newTestOrg(t)

	err := orgHandle.AddAdmin(ctx, adminDID)
	require.ErrorIs(t, err, org.ErrNotMember)
}

func TestRemoveAdmin_LastAdmin(t *testing.T) {
	ctx := context.Background()
	store, orgHandle := newTestOrg(t)

	id := addMember(t, store, orgHandle, "alice")
	require.NoError(t, orgHandle.AddAdmin(ctx, id.DID))

	err := orgHandle.RemoveAdmin(ctx, id.DID)
	require.ErrorIs(t, err, org.ErrLastAdmin)
}

func TestRemoveAdmin_MultipleAdmins(t *testing.T) {
	ctx := context.Background()
	store, orgHandle := newTestOrg(t)

	id1 := addMember(t, store, orgHandle, "alice")
	id2 := addMember(t, store, orgHandle, "bob")
	require.NoError(t, orgHandle.AddAdmin(ctx, id1.DID))
	require.NoError(t, orgHandle.AddAdmin(ctx, id2.DID))

	require.NoError(t, orgHandle.RemoveAdmin(ctx, id2.DID))

	admins, err := orgHandle.GetAdmins(ctx)
	require.NoError(t, err)
	require.Equal(t, []syntax.DID{id1.DID}, admins)
}

func TestGetMembers(t *testing.T) {
	ctx := context.Background()
	store, orgHandle := newTestOrg(t)

	members, err := orgHandle.GetMembers(ctx)
	require.NoError(t, err)
	require.Empty(t, members)

	id1 := addMember(t, store, orgHandle, "alice")
	id2 := addMember(t, store, orgHandle, "bob")

	members, err = orgHandle.GetMembers(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []syntax.DID{id1.DID, id2.DID}, members)
}

func TestRemoveMembers(t *testing.T) {
	ctx := context.Background()
	store, orgHandle := newTestOrg(t)

	id1 := addMember(t, store, orgHandle, "alice")
	id2 := addMember(t, store, orgHandle, "bob")
	require.NoError(t, orgHandle.RemoveMembers(ctx, []syntax.DID{id2.DID}))

	ok, err := orgHandle.IsMember(ctx, id1.DID)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = orgHandle.IsMember(ctx, id2.DID)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestAddAdmin_RemovesMemberFGA(t *testing.T) {
	ctx := context.Background()
	store, orgHandle := newTestOrg(t)

	id := addMember(t, store, orgHandle, "alice")
	memberUser := fgastore.MemberUserString(id.DID)
	orgObj := fgastore.OrgObjectKey(orgHandle.OrgID())

	// Before promotion: has member tuple, no admin tuple
	tuples, err := orgHandle.FGA().Read(ctx, fgastore.Tuple{
		User:     memberUser,
		Relation: fgastore.RelationMember,
		Object:   orgObj,
	})
	require.NoError(t, err)
	require.Len(t, tuples, 1)

	tuples, err = orgHandle.FGA().Read(ctx, fgastore.Tuple{
		User:     memberUser,
		Relation: fgastore.RelationAdmin,
		Object:   orgObj,
	})
	require.NoError(t, err)
	require.Empty(t, tuples)

	require.NoError(t, orgHandle.AddAdmin(ctx, id.DID))

	// After promotion: has admin tuple, no member tuple
	tuples, err = orgHandle.FGA().Read(ctx, fgastore.Tuple{
		User:     memberUser,
		Relation: fgastore.RelationAdmin,
		Object:   orgObj,
	})
	require.NoError(t, err)
	require.Len(t, tuples, 1)

	tuples, err = orgHandle.FGA().Read(ctx, fgastore.Tuple{
		User:     memberUser,
		Relation: fgastore.RelationMember,
		Object:   orgObj,
	})
	require.NoError(t, err)
	require.Empty(t, tuples)
}

func TestDowngradeAdmin(t *testing.T) {
	ctx := context.Background()
	store, orgHandle := newTestOrg(t)

	id1 := addMember(t, store, orgHandle, "alice")
	id2 := addMember(t, store, orgHandle, "bob")
	require.NoError(t, orgHandle.AddAdmin(ctx, id1.DID))
	require.NoError(t, orgHandle.AddAdmin(ctx, id2.DID))

	memberUser := fgastore.MemberUserString(id1.DID)
	orgObj := fgastore.OrgObjectKey(orgHandle.OrgID())

	require.NoError(t, orgHandle.DowngradeAdmin(ctx, id1.DID))

	// After downgrade: has member tuple, no admin tuple
	tuples, err := orgHandle.FGA().Read(ctx, fgastore.Tuple{
		User:     memberUser,
		Relation: fgastore.RelationMember,
		Object:   orgObj,
	})
	require.NoError(t, err)
	require.Len(t, tuples, 1)

	tuples, err = orgHandle.FGA().Read(ctx, fgastore.Tuple{
		User:     memberUser,
		Relation: fgastore.RelationAdmin,
		Object:   orgObj,
	})
	require.NoError(t, err)
	require.Empty(t, tuples)

	// DB-level verification
	ok, err := orgHandle.IsMember(ctx, id1.DID)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = orgHandle.IsAdmin(ctx, id1.DID)
	require.NoError(t, err)
	require.False(t, ok)

	admins, err := orgHandle.GetAdmins(ctx)
	require.NoError(t, err)
	require.Equal(t, []syntax.DID{id2.DID}, admins)
}

func TestDowngradeAdmin_LastAdmin(t *testing.T) {
	ctx := context.Background()
	store, orgHandle := newTestOrg(t)

	id := addMember(t, store, orgHandle, "alice")
	require.NoError(t, orgHandle.AddAdmin(ctx, id.DID))

	err := orgHandle.DowngradeAdmin(ctx, id.DID)
	require.ErrorIs(t, err, org.ErrLastAdmin)

	// Still an admin
	ok, err := orgHandle.IsAdmin(ctx, id.DID)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestRemoveMembers_RemovesFGATuples(t *testing.T) {
	ctx := context.Background()
	store, orgHandle := newTestOrg(t)

	id := addMember(t, store, orgHandle, "alice")
	memberUser := fgastore.MemberUserString(id.DID)
	orgObj := fgastore.OrgObjectKey(orgHandle.OrgID())

	require.NoError(t, orgHandle.RemoveMembers(ctx, []syntax.DID{id.DID}))

	// FGA member tuple should be gone
	tuples, err := orgHandle.FGA().Read(ctx, fgastore.Tuple{
		User:     memberUser,
		Relation: fgastore.RelationMember,
		Object:   orgObj,
	})
	require.NoError(t, err)
	require.Empty(t, tuples)
}

func TestGenerateAndUseIdentityToken(t *testing.T) {
	ctx := context.Background()
	store, orgHandle := newTestOrg(t)

	token, err := store.IssueIdentityToken(
		ctx,
		orgHandle.OrgID(),
		adminDID,
		false,
		time.Now().Add(time.Hour),
	)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	_, err = store.CreateNewMemberIdentity(
		ctx, orgHandle.OrgID(), token, "alice", testPasswordHash, "",
	)
	require.NoError(t, err)

	members, err := orgHandle.GetMembers(ctx)
	require.NoError(t, err)
	require.Len(t, members, 1)
}

func TestIdentityToken_CannotReuse(t *testing.T) {
	ctx := context.Background()
	store, orgHandle := newTestOrg(t)

	token, err := store.IssueIdentityToken(
		ctx,
		orgHandle.OrgID(),
		adminDID,
		false,
		time.Now().Add(time.Hour),
	)
	require.NoError(t, err)

	_, err = store.CreateNewMemberIdentity(
		ctx, orgHandle.OrgID(), token, "alice", testPasswordHash, "",
	)
	require.NoError(t, err)
	_, err = store.CreateNewMemberIdentity(
		ctx,
		orgHandle.OrgID(),
		token,
		"bob",
		testPasswordHash,
		"",
	)
	require.ErrorIs(t, err, org.ErrInvalidToken)
}

func TestMintIdentity_DuplicateHandle(t *testing.T) {
	ctx := context.Background()
	store, orgHandle := newTestOrg(t)

	token1, err := store.IssueIdentityToken(
		ctx,
		orgHandle.OrgID(),
		adminDID,
		false,
		time.Now().Add(time.Hour),
	)
	require.NoError(t, err)
	token2, err := store.IssueIdentityToken(
		ctx,
		orgHandle.OrgID(),
		adminDID,
		false,
		time.Now().Add(time.Hour),
	)
	require.NoError(t, err)

	_, err = store.CreateNewMemberIdentity(
		ctx,
		orgHandle.OrgID(),
		token1,
		"alice",
		testPasswordHash,
		"",
	)
	require.NoError(t, err)
	_, err = store.CreateNewMemberIdentity(
		ctx,
		orgHandle.OrgID(),
		token2,
		"alice",
		testPasswordHash,
		"",
	)
	require.ErrorIs(t, err, hive.ErrNotCreated)
}

func TestIdentityToken_Reusable(t *testing.T) {
	ctx := context.Background()
	store, orgHandle := newTestOrg(t)

	token, err := store.IssueIdentityToken(
		ctx,
		orgHandle.OrgID(),
		adminDID,
		true,
		time.Now().Add(time.Hour),
	)
	require.NoError(t, err)

	_, err = store.CreateNewMemberIdentity(
		ctx, orgHandle.OrgID(), token, "alice", testPasswordHash, "",
	)
	require.NoError(t, err)
	_, err = store.CreateNewMemberIdentity(
		ctx,
		orgHandle.OrgID(),
		token,
		"bob",
		testPasswordHash,
		"",
	)
	require.NoError(t, err)
	_, err = store.CreateNewMemberIdentity(
		ctx, orgHandle.OrgID(), token, "alice", testPasswordHash, "",
	)
	require.ErrorIs(t, err, hive.ErrNotCreated)
}

func TestIssueIdentityToken_ExpiryTooLate(t *testing.T) {
	ctx := context.Background()
	store, orgHandle := newTestOrg(t)

	_, err := store.IssueIdentityToken(
		ctx,
		orgHandle.OrgID(),
		adminDID,
		false,
		time.Now().AddDate(0, 1, 1),
	)
	require.ErrorIs(t, err, org.ErrInvalidTokenExpiry)
}
