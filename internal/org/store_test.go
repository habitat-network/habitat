package org

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	h, err := hive.NewHive("example.com", "pear.example.com", db)
	require.NoError(t, err)
	s, err := NewStore(db, h, identity.DefaultDirectory(), "pear.example.com")
	require.NoError(t, err)
	return s
}

func TestStore_CreateOrg(t *testing.T) {
	s := newTestStore(t)
	orgID, id, err := s.CreateOrg(t.Context(), "test-org", "admin", "password")
	require.NoError(t, err)
	require.NotEmpty(t, orgID)
	require.NotNil(t, id)
	require.Contains(t, id.Handle.String(), "admin")

	org, err := s.GetOrg(t.Context(), orgID)
	require.NoError(t, err)
	require.Equal(t, LoginMethodPassword, org.LoginMethod())

	members, err := org.GetMembers(t.Context())
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.Equal(t, id.DID, members[0])

	admins, err := org.GetAdmins(t.Context())
	require.NoError(t, err)
	require.Len(t, admins, 1)
	require.Equal(t, id.DID, admins[0])
}

func TestStore_GetOrg_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetOrg(t.Context(), "nonexistent")
	require.ErrorIs(t, err, ErrOrgNotFound)
}

func TestStore_GetOrgForDID_Member(t *testing.T) {
	s := newTestStore(t)
	orgID, id, err := s.CreateOrg(t.Context(), "test-org", "admin", "password")
	require.NoError(t, err)

	org, err := s.GetOrgForDID(t.Context(), id.DID)
	require.NoError(t, err)
	require.Equal(t, LoginMethodPassword, org.LoginMethod())

	gotOrgID := ""
	switch o := org.(type) {
	case *orgImpl:
		gotOrgID = o.orgID
	}
	require.Equal(t, orgID, gotOrgID)
}

func TestStore_GetOrgForDID_Everyone(t *testing.T) {
	s := newTestStore(t)
	unknown := syntax.DID("did:plc:unknown")
	org, err := s.GetOrgForDID(t.Context(), unknown)
	require.NoError(t, err)
	require.Equal(t, LoginMethodAtproto, org.LoginMethod())

	ok, err := org.IsMember(t.Context(), unknown)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestStore_GetMember_Existing(t *testing.T) {
	s := newTestStore(t)
	_, id, err := s.CreateOrg(t.Context(), "test-org", "admin", "password")
	require.NoError(t, err)

	member, err := s.GetMember(t.Context(), id.DID)
	require.NoError(t, err)
	require.Equal(t, id.DID, member.DID)
	require.Equal(t, AdminRole, member.Role)
	require.NotEmpty(t, member.LoginID)
}

func TestStore_GetMember_NotFound(t *testing.T) {
	s := newTestStore(t)
	unknown := syntax.DID("did:plc:unknown")
	member, err := s.GetMember(t.Context(), unknown)
	require.NoError(t, err)
	require.Equal(t, unknown, member.DID)
	require.Equal(t, MemberRole, member.Role)
	require.Empty(t, member.LoginID)

	ok, err := member.Org.IsMember(t.Context(), unknown)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestStore_GetOrgForDID_AfterMultipleOrgs(t *testing.T) {
	s := newTestStore(t)
	orgID1, id1, err := s.CreateOrg(t.Context(), "org1", "admin1", "password1")
	require.NoError(t, err)
	orgID2, id2, err := s.CreateOrg(t.Context(), "org2", "admin2", "password2")
	require.NoError(t, err)

	org, err := s.GetOrgForDID(t.Context(), id1.DID)
	require.NoError(t, err)
	gotID1 := ""
	switch o := org.(type) {
	case *orgImpl:
		gotID1 = o.orgID
	}
	require.Equal(t, orgID1, gotID1)

	org, err = s.GetOrgForDID(t.Context(), id2.DID)
	require.NoError(t, err)
	gotID2 := ""
	switch o := org.(type) {
	case *orgImpl:
		gotID2 = o.orgID
	}
	require.Equal(t, orgID2, gotID2)
}
