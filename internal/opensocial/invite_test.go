package opensocial_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	orgDID      = syntax.DID("did:plc:org")
	otherOrgDID = syntax.DID("did:plc:other-org")
	alice       = syntax.DID("did:plc:alice")
	bob         = syntax.DID("did:plc:bob")
)

func newTestStore(t *testing.T) (*opensocial.Store, spaces.Store) {
	t.Helper()
	return opensocial_testutil.NewTestStore(t)
}

// bootstrapMembersSpace creates the org's members space, mirroring the subset
// of Store.NewOrg's setup the invite flow depends on.
func bootstrapMembersSpace(
	t *testing.T,
	spacesStore spaces.Store,
	org syntax.DID,
) {
	t.Helper()
	_, err := spacesStore.CreateSpace(t.Context(), org, "community.opensocial.members", "self")
	require.NoError(t, err)
}

func TestCreateInvite_ListInvites_AcceptInvite(t *testing.T) {
	s, spacesStore := newTestStore(t)
	bootstrapMembersSpace(t, spacesStore, orgDID)

	invite, err := s.CreateInvite(
		t.Context(), orgDID, alice, []string{opensocial.MemberRoleRkey},
	)
	require.NoError(t, err)
	require.Equal(t, orgDID, invite.Org)
	require.Equal(t, alice, invite.Invitee)
	require.Equal(t, []string{opensocial.MemberRoleRkey}, invite.Roles)
	require.NotEmpty(t, invite.ID)

	invites, err := s.ListInvites(t.Context(), alice)
	require.NoError(t, err)
	require.Len(t, invites, 1)
	require.Equal(t, invite.ID, invites[0].ID)

	roles, err := s.AcceptInvite(t.Context(), orgDID, alice)
	require.NoError(t, err)
	require.Equal(t, []string{opensocial.MemberRoleRkey}, roles)

	// The invite is consumed.
	invites, err = s.ListInvites(t.Context(), alice)
	require.NoError(t, err)
	require.Empty(t, invites)

	// The invitee is now a member with the invited roles.
	memberRoles, err := s.GetUserRoles(t.Context(), orgDID, alice)
	require.NoError(t, err)
	require.Equal(t, []string{opensocial.MemberRoleRkey}, memberRoles)
}

func TestCreateInvite_AlreadyMember(t *testing.T) {
	s, spacesStore := newTestStore(t)
	bootstrapMembersSpace(t, spacesStore, orgDID)
	membersSpace := habitat_syntax.ConstructSpaceURI(orgDID, "community.opensocial.members", "self")

	_, _, err := spacesStore.PutRecord(
		t.Context(), membersSpace, orgDID, "community.opensocial.membership",
		syntax.RecordKey(alice),
		opensocial_api.CommunityOpensocialMembership{Roles: []string{opensocial.MemberRoleRkey}},
	)
	require.NoError(t, err)

	_, err = s.CreateInvite(t.Context(), orgDID, alice, nil)
	require.ErrorIs(t, err, opensocial.ErrAlreadyMember)
}

func TestCreateInvite_Duplicate(t *testing.T) {
	s, spacesStore := newTestStore(t)
	bootstrapMembersSpace(t, spacesStore, orgDID)

	_, err := s.CreateInvite(t.Context(), orgDID, alice, nil)
	require.NoError(t, err)

	_, err = s.CreateInvite(t.Context(), orgDID, alice, nil)
	require.ErrorIs(t, err, opensocial.ErrInviteAlreadyExists)
}

func TestRevokeInvite(t *testing.T) {
	s, spacesStore := newTestStore(t)
	bootstrapMembersSpace(t, spacesStore, orgDID)

	invite, err := s.CreateInvite(t.Context(), orgDID, alice, nil)
	require.NoError(t, err)

	require.NoError(t, s.RevokeInvite(t.Context(), orgDID, invite.ID))

	invites, err := s.ListPendingInvites(t.Context(), orgDID)
	require.NoError(t, err)
	require.Empty(t, invites)

	err = s.RevokeInvite(t.Context(), orgDID, invite.ID)
	require.ErrorIs(t, err, opensocial.ErrInviteNotFound)
}

func TestAcceptInvite_NotFound(t *testing.T) {
	s, spacesStore := newTestStore(t)
	bootstrapMembersSpace(t, spacesStore, orgDID)

	_, err := s.AcceptInvite(t.Context(), orgDID, alice)
	require.ErrorIs(t, err, opensocial.ErrInviteNotFound)
}

// TestAcceptInvite_AlreadyMemberNoInvite covers the org creator's flow: they
// hold a membership record (granted directly by NewOrg) but no invite row,
// since they were never invited. AcceptInvite should confirm it — used by
// requestJoin, called under the creator's own credentials — rather than error.
func TestAcceptInvite_AlreadyMemberNoInvite(t *testing.T) {
	s, spacesStore := newTestStore(t)
	bootstrapMembersSpace(t, spacesStore, orgDID)
	membersSpace := habitat_syntax.ConstructSpaceURI(orgDID, "community.opensocial.members", "self")

	_, _, err := spacesStore.PutRecord(
		t.Context(), membersSpace, orgDID, "community.opensocial.membership",
		syntax.RecordKey(alice),
		opensocial_api.CommunityOpensocialMembership{Roles: []string{opensocial.AdminRoleRkey}},
	)
	require.NoError(t, err)

	roles, err := s.AcceptInvite(t.Context(), orgDID, alice)
	require.NoError(t, err)
	require.Equal(t, []string{opensocial.AdminRoleRkey}, roles)
}

func TestListPendingInvitesAndListInvites_ScopedByOrgAndInvitee(t *testing.T) {
	s, spacesStore := newTestStore(t)
	bootstrapMembersSpace(t, spacesStore, orgDID)
	bootstrapMembersSpace(t, spacesStore, otherOrgDID)

	_, err := s.CreateInvite(t.Context(), orgDID, alice, nil)
	require.NoError(t, err)
	_, err = s.CreateInvite(t.Context(), orgDID, bob, nil)
	require.NoError(t, err)
	_, err = s.CreateInvite(t.Context(), otherOrgDID, alice, nil)
	require.NoError(t, err)

	pending, err := s.ListPendingInvites(t.Context(), orgDID)
	require.NoError(t, err)
	require.Len(t, pending, 2)

	// Alice's invites span both orgs; bob's is just the one.
	aliceAllOrgs, err := s.ListInvites(t.Context(), alice)
	require.NoError(t, err)
	require.Len(t, aliceAllOrgs, 2)

	bobAllOrgs, err := s.ListInvites(t.Context(), bob)
	require.NoError(t, err)
	require.Len(t, bobAllOrgs, 1)
	require.Equal(t, orgDID, bobAllOrgs[0].Org)
}
