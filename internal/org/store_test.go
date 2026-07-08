package org

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *storeImpl {
	t.Helper()
	db := testutil.NewDB(t)
	h, err := hive.NewHive("example.com", "pear.example.com", db)
	require.NoError(t, err)
	passwordProvider, err := login.NewPasswordProvider(
		db,
		"pear.example.com",
		[]byte("test-signing-secret-for-org-00000"),
		pdsclient.NewDummyDirectory("https://pds.example.com"),
	)
	require.NoError(t, err)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	store, err := NewStore(
		db,
		h,
		pdsclient.NewDummyDirectory("https://pds.example.com"),
		"pear.example.com",
		passwordProvider,
		fga,
	)
	require.NoError(t, err)
	return store.(*storeImpl)
}

func TestStore_CreateOrg(t *testing.T) {
	s := newTestStore(t)
	orgId, adminId, err := s.CreateOrg(
		t.Context(),
		"test-org",
		"admin",
		"password",
		"password",
		"",
		"",
		"contact@example.com",
	)
	require.NoError(t, err)
	require.NotNil(t, orgId)
	require.NotNil(t, adminId)
	require.Contains(t, adminId.Handle.String(), "admin")

	org, err := s.GetOrg(t.Context(), orgId.DID)
	require.NoError(t, err)

	members, err := org.GetMembers(t.Context())
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.Equal(t, adminId.DID, members[0])

	admins, err := org.GetAdmins(t.Context())
	require.NoError(t, err)
	require.Len(t, admins, 1)
	require.Equal(t, adminId.DID, admins[0])

	var stored organization
	require.NoError(t, s.db.First(&stored, "id = ?", orgId.DID).Error)
	require.Equal(t, "contact@example.com", stored.ContactEmail)
}

func TestStore_GetOrg_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetOrg(t.Context(), syntax.DID("did:web:nonexistent"))
	require.ErrorIs(t, err, ErrOrgNotFound)
}

func TestStore_GetOrgForDID_Member(t *testing.T) {
	s := newTestStore(t)
	orgId, adminId, err := s.CreateOrg(
		t.Context(),
		"test-org",
		"admin",
		"password",
		"password",
		"",
		"",
		"contact@example.com",
	)
	require.NoError(t, err)

	org, _, err := s.GetOrgForDID(t.Context(), adminId.DID)
	require.NoError(t, err)

	var gotOrgID syntax.DID
	switch o := org.(type) {
	case *orgImpl:
		gotOrgID = o.orgID
	}
	require.Equal(t, orgId.DID, gotOrgID)
}

func TestStore_GetOrgForDID_Everyone(t *testing.T) {
	s := newTestStore(t)
	// Not a member of any org and not hive-managed; the dummy directory
	// resolves it with no "habitat" service, so it falls through to the
	// everyone org.
	external := syntax.DID("did:plc:unknown")
	org, _, err := s.GetOrgForDID(t.Context(), external)
	require.NoError(t, err)
	_, ok := org.(*EveryoneOrg)
	require.True(t, ok)
}

func TestStore_GetMember_Existing(t *testing.T) {
	s := newTestStore(t)
	orgId, adminId, err := s.CreateOrg(
		t.Context(),
		"test-org",
		"admin",
		"password",
		"password",
		"",
		"",
		"contact@example.com",
	)
	require.NoError(t, err)

	member, err := s.GetMember(t.Context(), adminId.DID)
	require.NoError(t, err)
	require.Equal(t, adminId.DID, member.DID)
	require.Equal(t, AdminRole, member.Role)
	require.Equal(t, orgId.DID, member.Org.DID())
	require.NotEmpty(t, member.LoginID)
}

func TestStore_GetMember_Public(t *testing.T) {
	s := newTestStore(t)
	unknown := syntax.DID("did:plc:unknown")
	member, err := s.GetMember(t.Context(), unknown)
	require.NoError(t, err)
	require.Equal(t, unknown, member.DID)
	require.Equal(t, MemberRole, member.Role)
	require.Equal(t, "did:plc:unknown", member.LoginID)

	ok, err := member.Org.IsMember(t.Context(), unknown)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestStore_GetOrgForDID_AfterMultipleOrgs(t *testing.T) {
	s := newTestStore(t)
	orgId1, adminId1, err := s.CreateOrg(
		t.Context(),
		"org1",
		"admin1",
		"password1",
		"password",
		"",
		"org1",
		"contact1@example.com",
	)
	require.NoError(t, err)
	orgId2, adminId2, err := s.CreateOrg(
		t.Context(),
		"org2",
		"admin2",
		"password2",
		"password",
		"",
		"org2",
		"contact2@example.com",
	)
	require.NoError(t, err)

	org, _, err := s.GetOrgForDID(t.Context(), adminId1.DID)
	require.NoError(t, err)
	var gotID1 syntax.DID
	switch o := org.(type) {
	case *orgImpl:
		gotID1 = o.orgID
	}
	require.Equal(t, orgId1.DID, gotID1)

	org, _, err = s.GetOrgForDID(t.Context(), adminId2.DID)
	require.NoError(t, err)
	var gotID2 syntax.DID
	switch o := org.(type) {
	case *orgImpl:
		gotID2 = o.orgID
	}
	require.Equal(t, orgId2.DID, gotID2)
}
