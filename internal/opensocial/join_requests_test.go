package opensocial_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
)

func TestStoreJoinRequests(t *testing.T) {
	ctx := t.Context()
	s := opensocial_testutil.NewTestStore(t)
	org, err := s.NewOrg(ctx, "acme", "did:plc:admin")
	require.NoError(t, err)
	group := syntax.DID(org)
	people := []syntax.DID{"did:plc:a", "did:plc:b", "did:plc:c"}

	t.Run("a request is kept once and can be fetched and denied", func(t *testing.T) {
		first, err := s.QueueJoinRequest(ctx, group, people[0], "let me in")
		require.NoError(t, err)
		again, err := s.QueueJoinRequest(ctx, group, people[0], "different")
		require.NoError(t, err)
		require.Equal(t, first, again, "asking again returns the existing request")

		got, err := s.GetJoinRequest(ctx, group, people[0])
		require.NoError(t, err)
		require.Equal(t, "let me in", got.Message)

		require.NoError(t, s.DeleteJoinRequest(ctx, group, people[0]))
		_, err = s.GetJoinRequest(ctx, group, people[0])
		require.ErrorIs(t, err, opensocial.ErrJoinRequestNotFound)
		require.ErrorIs(t, s.DeleteJoinRequest(ctx, group, people[0]),
			opensocial.ErrJoinRequestNotFound)
	})

	t.Run("listing pages oldest first and drops admitted members", func(t *testing.T) {
		for _, person := range people {
			_, err := s.QueueJoinRequest(ctx, group, person, "")
			require.NoError(t, err)
		}
		require.NoError(t, s.AssignRoles(ctx, group, people[1], []string{opensocial.MemberRoleRkey}))

		var seen []syntax.DID
		cursor := ""
		for range 3 {
			page, next, err := s.ListJoinRequests(ctx, group, 1, cursor)
			require.NoError(t, err)
			for _, request := range page {
				seen = append(seen, request.Member)
			}
			if next == "" {
				break
			}
			cursor = next
		}
		require.ElementsMatch(t, []syntax.DID{people[0], people[2]}, seen)

		_, err := s.GetJoinRequest(ctx, group, people[1])
		require.ErrorIs(t, err, opensocial.ErrJoinRequestNotFound, "the admitted request is gone")
	})

	t.Run("a cursor the host did not issue is refused", func(t *testing.T) {
		_, _, err := s.ListJoinRequests(ctx, group, 10, "junk")
		require.ErrorIs(t, err, opensocial.ErrInvalidCursor)
	})
}
