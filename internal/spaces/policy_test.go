package spaces_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
)

func TestParsePolicy(t *testing.T) {
	t.Run("empty is the default memberspace", func(t *testing.T) {
		p, err := spaces.ParsePolicy(nil)
		require.NoError(t, err)
		require.Equal(t, spaces.DefaultPolicy(), p)
	})
	t.Run("memberspace keeps its configuration", func(t *testing.T) {
		p, err := spaces.ParsePolicy(json.RawMessage(
			`{"$type":"com.atproto.memberspace.policy","publicRead":true,"allowedApps":["a"]}`))
		require.NoError(t, err)
		require.True(t, p.PublicRead)
		require.Equal(t, []string{"a"}, p.AllowedApps)
	})
	t.Run("personalspace drops configuration", func(t *testing.T) {
		p, err := spaces.ParsePolicy(json.RawMessage(
			`{"$type":"com.atproto.personalspace.policy","publicRead":true}`))
		require.NoError(t, err)
		require.Equal(t, spaces.Policy{Type: spaces.PolicyPersonalSpace}, p)
	})
	t.Run("managedspace and unknown policies are unsupported", func(t *testing.T) {
		for _, raw := range []string{
			`{"$type":"com.atproto.managedspace.policy","managingApp":"did:web:x"}`,
			`{"$type":"com.example.other"}`,
		} {
			_, err := spaces.ParsePolicy(json.RawMessage(raw))
			require.ErrorIs(t, err, spaces.ErrUnsupportedPolicy)
		}
	})
	t.Run("missing type or bad json is invalid", func(t *testing.T) {
		for _, raw := range []string{`{}`, `[1]`} {
			_, err := spaces.ParsePolicy(json.RawMessage(raw))
			require.ErrorIs(t, err, spaces.ErrInvalidPolicy)
		}
	})
}

func TestSpacePolicy_RoundTrip(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)
	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "pol")
	require.NoError(t, err)

	got, err := s.GetSpacePolicy(t.Context(), uri)
	require.NoError(t, err)
	require.Equal(t, spaces.DefaultPolicy(), got, "new spaces default to memberspace")

	want := spaces.Policy{
		Type:        spaces.PolicyMemberSpace,
		PublicRead:  true,
		AllowedApps: []string{"x"},
	}
	require.NoError(t, s.SetSpacePolicy(t.Context(), uri, want))
	got, err = s.GetSpacePolicy(t.Context(), uri)
	require.NoError(t, err)
	require.Equal(t, want, got)

	other, err := s.CreateSpace(t.Context(), orgID, groupType, "missing")
	require.NoError(t, err)
	require.NoError(t, s.DeleteSpace(t.Context(), other))
	require.ErrorIs(t, s.SetSpacePolicy(t.Context(), other, want), spaces.ErrSpaceNotFound)
	_, err = s.GetSpacePolicy(t.Context(), other)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
}
