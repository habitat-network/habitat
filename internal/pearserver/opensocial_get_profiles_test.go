package pearserver_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestServer_GetProfiles(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("requires membership", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var out habitat.NetworkHabitatOpensocialGetProfilesOutput
		code := client.Query(
			ts.Server.GetProfiles,
			url.Values{"org": {orgDID}, "dids": {string(admin)}},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("returns member profiles and omits non-members", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		org := syntax.DID(orgDID)
		require.NoError(t, ts.OpenSocialStore.AssignRoles(
			t.Context(), org, alice, []string{opensocial.MemberRoleRkey},
		))

		membersSpace := habitat_syntax.ConstructSpaceURI(
			org, "community.opensocial.members", "self",
		)
		_, _, err = ts.SpaceStore.PutRecord(
			t.Context(), membersSpace, admin, "community.opensocial.memberProfile",
			syntax.RecordKey(admin),
			spaces_testutil.MustMarshalRecord(t, opensocial_api.CommunityOpensocialMemberProfile{
				DisplayName: "Admin Person",
				UpdatedAt:   "2024-01-01T00:00:00Z",
			}),
		)
		require.NoError(t, err)
		_, _, err = ts.SpaceStore.PutRecord(
			t.Context(), membersSpace, alice, "community.opensocial.memberProfile",
			syntax.RecordKey(alice),
			spaces_testutil.MustMarshalRecord(t, opensocial_api.CommunityOpensocialMemberProfile{
				DisplayName: "Alice Person",
				Bio:         "Hi, I'm Alice",
				UpdatedAt:   "2024-01-01T00:00:00Z",
			}),
		)
		require.NoError(t, err)

		notAMember := syntax.DID("did:plc:notamember")
		var out habitat.NetworkHabitatOpensocialGetProfilesOutput
		code := client.Query(
			ts.Server.GetProfiles,
			url.Values{
				"org":  {orgDID},
				"dids": {string(admin), string(alice), string(notAMember)},
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
		require.Len(t, out.Profiles, 2)

		byDID := make(map[string]habitat.NetworkHabitatOpensocialGetProfilesProfileView)
		for _, p := range out.Profiles {
			byDID[p.Did] = p
		}
		require.Equal(t, "Admin Person", byDID[admin.String()].DisplayName)
		require.Equal(t, "Alice Person", byDID[alice.String()].DisplayName)
		require.Equal(t, "Hi, I'm Alice", byDID[alice.String()].Bio)
		_, ok := byDID[notAMember.String()]
		require.False(t, ok)
	})
}
