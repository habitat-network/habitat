package opensocial_test

import (
	"encoding/json"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// TestStore exercises the opensocial store end to end on a single org, shared
// across the nested tests. The org is minted once at the top level, which is
// also where NewOrg's bootstrap behavior is asserted; subtests then reuse that
// org.
func TestStore(t *testing.T) {
	s := opensocial_testutil.NewTestStore(t)
	creator := syntax.DID("did:plc:creator")

	orgDIDStr, err := s.NewOrg(t.Context(), "acme", creator)
	require.NoError(t, err)
	require.NotEmpty(t, orgDIDStr)
	org := syntax.DID(orgDIDStr)

	// NewOrg bootstraps the creator as an admin.
	roles, err := s.GetUserRoles(t.Context(), org, creator)
	require.NoError(t, err)
	require.Equal(t, []string{opensocial.AdminRoleRkey}, roles)

	// The about space carries the org's profile.
	_, err = s.SpaceStore.GetRecord(
		t.Context(),
		habitat_syntax.ConstructSpaceURI(org, "community.opensocial.about", "self"),
		org, "community.opensocial.profile", "self",
	)
	require.NoError(t, err)

	// The members space carries both bootstrap roles.
	membersSpace := habitat_syntax.ConstructSpaceURI(org, "community.opensocial.members", "self")
	_, err = s.SpaceStore.GetRecord(
		t.Context(), membersSpace, org, "community.opensocial.role", opensocial.AdminRoleRkey,
	)
	require.NoError(t, err)
	_, err = s.SpaceStore.GetRecord(
		t.Context(), membersSpace, org, "community.opensocial.role", opensocial.MemberRoleRkey,
	)
	require.NoError(t, err)

	// The about space's access record grants member+admin, so a member with
	// the member role passes CheckPermission. Shared by the role/permission
	// tests below.
	member := syntax.DID("did:plc:member")
	require.NoError(t, s.AssignRoles(t.Context(), org, member, []string{opensocial.MemberRoleRkey}))

	t.Run("UpdateProfile", func(t *testing.T) {
		aboutSpace := habitat_syntax.ConstructSpaceURI(org, "community.opensocial.about", "self")

		require.NoError(t, s.UpdateProfile(t.Context(), org, "Acme Corp", "We make widgets", ""))

		record, err := s.SpaceStore.GetRecord(
			t.Context(), aboutSpace, org, "community.opensocial.profile", "self",
		)
		require.NoError(t, err)
		require.Equal(t, "Acme Corp", record.Value["name"])
		require.Equal(t, "We make widgets", record.Value["description"])

		// Clearing the description removes the field rather than leaving an
		// empty string.
		require.NoError(t, s.UpdateProfile(t.Context(), org, "Acme Corp", "", ""))
		record, err = s.SpaceStore.GetRecord(
			t.Context(), aboutSpace, org, "community.opensocial.profile", "self",
		)
		require.NoError(t, err)
		require.NotContains(t, record.Value, "description")
	})

	t.Run("CreateSpace", func(t *testing.T) {
		spaceURI, err := s.CreateSpace(
			t.Context(),
			org,
			[]string{opensocial.MemberRoleRkey},
			syntax.NSID("community.opensocial.channel"),
			"general",
		)
		require.NoError(t, err)
		require.NotEmpty(t, spaceURI)

		exists, err := s.SpaceStore.CheckSpaceExists(t.Context(), spaceURI)
		require.NoError(t, err)
		require.True(t, exists)

		// The space carries an access record with the requested roles.
		record, err := s.SpaceStore.GetRecord(
			t.Context(), spaceURI, org, "community.opensocial.access", "self",
		)
		require.NoError(t, err)
		require.Equal(t, []any{opensocial.MemberRoleRkey}, record.Value["roles"])
	})

	t.Run("UploadImage", func(t *testing.T) {
		png := []byte("fake-png-bytes")
		require.NoError(t, s.UpdateProfile(t.Context(), org, "Brand New", "", ""))

		blob, err := s.UploadImage(t.Context(), org, "image/png", png)
		require.NoError(t, err)
		require.Equal(t, "image/png", blob.MimeType)
		require.Equal(t, int64(len(png)), blob.Size)

		// The avatar is attached to the org's profile record, preserving the
		// rest of the record (here the name set by UpdateProfile above).
		aboutSpace := habitat_syntax.ConstructSpaceURI(org, "community.opensocial.about", "self")
		record, err := s.SpaceStore.GetRecord(
			t.Context(), aboutSpace, org, "community.opensocial.profile", "self",
		)
		require.NoError(t, err)
		require.Contains(t, record.Value, "avatar")

		var profile opensocial_api.CommunityOpensocialProfile
		raw, err := json.Marshal(record.Value)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &profile))
		require.Equal(t, "Brand New", profile.Name)
		require.NotNil(t, profile.Avatar)
		require.Equal(t, "image/png", profile.Avatar.MimeType)
		require.Equal(t, int64(len(png)), profile.Avatar.Size)
	})

	t.Run("AssignRoles", func(t *testing.T) {
		other := syntax.DID("did:plc:other")
		require.NoError(
			t,
			s.AssignRoles(t.Context(), org, other, []string{opensocial.MemberRoleRkey}),
		)

		roles, err := s.GetUserRoles(t.Context(), org, other)
		require.NoError(t, err)
		require.Equal(t, []string{opensocial.MemberRoleRkey}, roles)
	})

	t.Run("CheckPermission", func(t *testing.T) {
		aboutSpace := habitat_syntax.ConstructSpaceURI(org, "community.opensocial.about", "self")

		ok, err := s.CheckPermission(t.Context(), member, aboutSpace)
		require.NoError(t, err)
		require.True(t, ok)

		// A stranger holds no roles, so permission is denied.
		ok, err = s.CheckPermission(t.Context(), syntax.DID("did:plc:stranger"), aboutSpace)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("GetUserRoles", func(t *testing.T) {
		roles, err := s.GetUserRoles(t.Context(), org, member)
		require.NoError(t, err)
		require.Equal(t, []string{opensocial.MemberRoleRkey}, roles)

		// An unknown member has no roles.
		roles, err = s.GetUserRoles(t.Context(), org, syntax.DID("did:plc:stranger"))
		require.NoError(t, err)
		require.Nil(t, roles)
	})

	t.Run("GetProfile", func(t *testing.T) {
		// The bootstrap org carries a profile record (name may have been
		// mutated by the earlier UpdateProfile/UploadImage subtests).
		profile, err := s.GetProfile(t.Context(), org)
		require.NoError(t, err)
		require.NotEmpty(t, profile.Name)

		// An org with no profile record returns a zero value rather than an
		// error.
		empty := syntax.DID("did:plc:no-profile")
		profile, err = s.GetProfile(t.Context(), empty)
		require.NoError(t, err)
		require.Empty(t, profile.Name)
	})

	t.Run("IsOrg", func(t *testing.T) {
		// The members space exists for a real org.
		ok, err := s.IsOrg(t.Context(), org)
		require.NoError(t, err)
		require.True(t, ok)

		// A DID with no members space is not an org.
		ok, err = s.IsOrg(t.Context(), syntax.DID("did:plc:not-an-org"))
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("AppAccess", func(t *testing.T) {
		const clientID = "https://app.example.com"

		// Nothing is granted until GrantAppAccess is called.
		ok, err := s.CheckAppAccess(t.Context(), org, clientID)
		require.NoError(t, err)
		require.False(t, ok)

		require.NoError(t, s.GrantAppAccess(t.Context(), org, clientID))

		// The grant is now visible, and only for that client_id.
		ok, err = s.CheckAppAccess(t.Context(), org, clientID)
		require.NoError(t, err)
		require.True(t, ok)

		ok, err = s.CheckAppAccess(t.Context(), org, "https://other.example.com")
		require.NoError(t, err)
		require.False(t, ok)
	})
}
