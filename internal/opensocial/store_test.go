package opensocial_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestNewOrg(t *testing.T) {
	s, spacesStore := opensocial_testutil.NewTestStore(t)
	creator := syntax.DID("did:plc:creator")

	orgDIDStr, err := s.NewOrg(t.Context(), "acme", creator)
	require.NoError(t, err)
	require.NotEmpty(t, orgDIDStr)
	org := syntax.DID(orgDIDStr)

	// The creator is bootstrapped as an admin.
	roles, err := s.GetUserRoles(t.Context(), org, creator)
	require.NoError(t, err)
	require.Equal(t, []string{opensocial.AdminRoleRkey}, roles)

	// The about space carries the org's profile.
	_, err = spacesStore.GetRecord(
		t.Context(),
		habitat_syntax.ConstructSpaceURI(org, "community.opensocial.about", "self"),
		org, "community.opensocial.profile", "self",
	)
	require.NoError(t, err)

	// The members space carries both bootstrap roles.
	membersSpace := habitat_syntax.ConstructSpaceURI(org, "community.opensocial.members", "self")
	_, err = spacesStore.GetRecord(
		t.Context(), membersSpace, org, "community.opensocial.role", opensocial.AdminRoleRkey,
	)
	require.NoError(t, err)
	_, err = spacesStore.GetRecord(
		t.Context(), membersSpace, org, "community.opensocial.role", opensocial.MemberRoleRkey,
	)
	require.NoError(t, err)
}

func TestUpdateProfile(t *testing.T) {
	s, spacesStore := opensocial_testutil.NewTestStore(t)
	creator := syntax.DID("did:plc:creator")

	orgDIDStr, err := s.NewOrg(t.Context(), "acme", creator)
	require.NoError(t, err)
	org := syntax.DID(orgDIDStr)
	aboutSpace := habitat_syntax.ConstructSpaceURI(org, "community.opensocial.about", "self")

	require.NoError(t, s.UpdateProfile(t.Context(), org, "Acme Corp", "We make widgets", ""))

	record, err := spacesStore.GetRecord(
		t.Context(), aboutSpace, org, "community.opensocial.profile", "self",
	)
	require.NoError(t, err)
	require.Equal(t, "Acme Corp", record.Value["name"])
	require.Equal(t, "We make widgets", record.Value["description"])

	// Clearing the description removes the field rather than leaving an
	// empty string.
	require.NoError(t, s.UpdateProfile(t.Context(), org, "Acme Corp", "", ""))
	record, err = spacesStore.GetRecord(
		t.Context(), aboutSpace, org, "community.opensocial.profile", "self",
	)
	require.NoError(t, err)
	require.NotContains(t, record.Value, "description")
}
