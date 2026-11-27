package opensocial_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/opensocial"
)

func TestJoinRequests_CreateListDelete(t *testing.T) {
	s := newTestStore(t)
	bootstrapMembersSpace(t, s.SpaceStore, orgDID)

	_, err := s.CreateJoinRequest(t.Context(), orgDID, alice, "let me in")
	require.NoError(t, err)
	_, err = s.CreateJoinRequest(t.Context(), orgDID, bob, "me too")
	require.NoError(t, err)
	// A repeat request replaces the message without duplicating.
	_, err = s.CreateJoinRequest(t.Context(), orgDID, alice, "please")
	require.NoError(t, err)

	all, err := s.ListJoinRequests(t.Context(), orgDID, 10, nil)
	require.NoError(t, err)
	require.Len(t, all, 2)
	require.Equal(t, "please", all[0].Message)

	// Cursor pagination resumes after the last request seen.
	next, err := s.ListJoinRequests(t.Context(), orgDID, 10, &all[0])
	require.NoError(t, err)
	require.Len(t, next, 1)
	require.Equal(t, bob, next[0].Member)

	require.NoError(t, s.DeleteJoinRequest(t.Context(), orgDID, alice))
	require.ErrorIs(
		t, s.DeleteJoinRequest(t.Context(), orgDID, alice), opensocial.ErrJoinRequestNotFound,
	)
	left, err := s.ListJoinRequests(t.Context(), orgDID, 10, nil)
	require.NoError(t, err)
	require.Len(t, left, 1)
}
