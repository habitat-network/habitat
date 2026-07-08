package org_test

import (
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/org/testutil"
	"github.com/stretchr/testify/require"
)

func TestStore_CreateOrg(t *testing.T) {
	s := testutil.NewTestStore(t)
	orgID, adminID, err := s.CreateOrg(
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
	require.NotNil(t, orgID)
	require.NotNil(t, adminID)
	require.Contains(t, adminID.Handle.String(), "admin")

	o, err := s.GetOrg(t.Context(), orgID.DID)
	require.NoError(t, err)

	members, err := o.GetMembers(t.Context())
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.Equal(t, adminID.DID, members[0])

	admins, err := o.GetAdmins(t.Context())
	require.NoError(t, err)
	require.Len(t, admins, 1)
	require.Equal(t, adminID.DID, admins[0])
}

func TestStore_GetOrg_NotFound(t *testing.T) {
	s := testutil.NewTestStore(t)
	_, err := s.GetOrg(t.Context(), syntax.DID("did:web:nonexistent"))
	require.ErrorIs(t, err, org.ErrOrgNotFound)
}

func TestStore_GetOrgForDID_Member(t *testing.T) {
	s := testutil.NewTestStore(t)
	orgID, adminID, err := s.CreateOrg(
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

	o, _, err := s.GetOrgForDID(t.Context(), adminID.DID)
	require.NoError(t, err)

	require.Equal(t, orgID.DID, o.DID())
}

func TestStore_GetOrgForDID_Everyone(t *testing.T) {
	s := testutil.NewTestStore(t)
	// Not a member of any org and not hive-managed; the dummy directory
	// resolves it with no "habitat" service, so it falls through to the
	// everyone org.
	external := syntax.DID("did:plc:unknown")
	o, _, err := s.GetOrgForDID(t.Context(), external)
	require.NoError(t, err)

	require.Equal(t, new(org.EveryoneOrg{}).DID(), o.DID())
}

func TestStore_GetMember_Existing(t *testing.T) {
	s := testutil.NewTestStore(t)
	orgID, adminID, err := s.CreateOrg(
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

	member, err := s.GetMember(t.Context(), adminID.DID)
	require.NoError(t, err)
	require.Equal(t, adminID.DID, member.DID)
	require.Equal(t, org.AdminRole, member.Role)
	require.Equal(t, orgID.DID, member.Org.DID())
	require.NotEmpty(t, member.LoginID)
}

func TestStore_GetMember_Public(t *testing.T) {
	s := testutil.NewTestStore(t)
	unknown := syntax.DID("did:plc:unknown")
	member, err := s.GetMember(t.Context(), unknown)
	require.NoError(t, err)
	require.Equal(t, unknown, member.DID)
	require.Equal(t, org.MemberRole, member.Role)
	require.Equal(t, "did:plc:unknown", member.LoginID)

	ok, err := member.Org.IsMember(t.Context(), unknown)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestStore_GetOrgForDID_AfterMultipleOrgs(t *testing.T) {
	s := testutil.NewTestStore(t)
	orgID1, adminID1, err := s.CreateOrg(
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
	orgID2, adminID2, err := s.CreateOrg(
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

	o, _, err := s.GetOrgForDID(t.Context(), adminID1.DID)
	require.NoError(t, err)
	require.Equal(t, orgID1.DID, o.DID())

	o, _, err = s.GetOrgForDID(t.Context(), adminID2.DID)
	require.NoError(t, err)
	require.Equal(t, orgID2.DID, o.DID())
}

func TestStore(t *testing.T) {
	s := testutil.NewTestStore(t)
	orgID, adminID, err := s.CreateOrg(
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

	t.Run("reusable", func(t *testing.T) {
		token, err := s.IssueIdentityToken(
			t.Context(),
			orgID.DID,
			adminID.DID,
			true,
			time.Now().Add(time.Hour),
		)
		require.NoError(t, err)

		require.NoError(t, s.ValidateAdminSignedToken(t.Context(), orgID.DID, token))
		require.NoError(t, s.ValidateAdminSignedToken(t.Context(), orgID.DID, token))
	})

	t.Run("non reusable", func(t *testing.T) {
		token, err := s.IssueIdentityToken(
			t.Context(),
			orgID.DID,
			adminID.DID,
			false,
			time.Now().Add(time.Hour),
		)
		require.NoError(t, err)

		require.NoError(t, s.ValidateAdminSignedToken(t.Context(), orgID.DID, token))
		require.ErrorIs(
			t,
			s.ValidateAdminSignedToken(t.Context(), orgID.DID, token),
			org.ErrInvalidToken,
		)
	})
}
