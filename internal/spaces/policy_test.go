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
	t.Run("relationspace drops configuration", func(t *testing.T) {
		p, err := spaces.ParsePolicy(json.RawMessage(
			`{"$type":"network.habitat.relationspace.policy","publicRead":true}`))
		require.NoError(t, err)
		require.Equal(t, spaces.Policy{Type: spaces.PolicyRelationSpace}, p)
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

func TestSpaceDeleteKeepsSpacesOfOtherTypes(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)
	kept, err := s.CreateSpace(t.Context(), orgID, groupType, "same-key")
	require.NoError(t, err)
	gone, err := s.CreateSpace(t.Context(), orgID, "network.habitat.other", "same-key")
	require.NoError(t, err)

	require.NoError(t, s.DeleteSpace(t.Context(), gone))
	exists, err := s.CheckSpaceExists(t.Context(), kept)
	require.NoError(t, err)
	require.True(t, exists, "a space with the same owner and skey but another type survives")
}

func TestParsePolicyGroupSpace(t *testing.T) {
	t.Run(
		"grants keep their roles and the policy reports who may read and write",
		func(t *testing.T) {
			p, err := spaces.ParsePolicy(json.RawMessage(`{
			"$type": "org.atgrp.policy", "publicRead": true, "unlisted": true,
			"grants": [
				{"role": "member", "read": true},
				{"role": "editor", "read": true, "write": true},
				{"role": "auditor", "permissions": ["include:org.atgrp.authModerate"]}
			]}`))
			require.NoError(t, err)
			require.True(t, p.PublicRead)
			require.True(t, p.Unlisted)
			require.Equal(t, []string{"member", "editor"}, p.ReadRoles())
			require.Equal(t, []string{"editor"}, p.WriteRoles())
		},
	)

	t.Run("a role may appear in one grant only", func(t *testing.T) {
		_, err := spaces.ParsePolicy(json.RawMessage(
			`{"$type":"org.atgrp.policy","grants":[{"role":"a"},{"role":"a"}]}`))
		require.ErrorIs(t, err, spaces.ErrInvalidPolicy)
		_, err = spaces.ParsePolicy(json.RawMessage(
			`{"$type":"org.atgrp.policy","grants":[{"read":true}]}`))
		require.ErrorIs(t, err, spaces.ErrInvalidPolicy)
	})

	t.Run("other policies are validated by type", func(t *testing.T) {
		_, err := spaces.ParsePolicy(json.RawMessage(`{}`))
		require.ErrorIs(t, err, spaces.ErrInvalidPolicy)
		_, err = spaces.ParsePolicy(json.RawMessage(`not json`))
		require.ErrorIs(t, err, spaces.ErrInvalidPolicy)
		_, err = spaces.ParsePolicy(json.RawMessage(`{"$type":"com.atproto.managedspace.policy"}`))
		require.ErrorIs(t, err, spaces.ErrUnsupportedPolicy)
		_, err = spaces.ParsePolicy(json.RawMessage(`{"$type":"x.y.z"}`))
		require.ErrorIs(t, err, spaces.ErrUnsupportedPolicy)
		p, err := spaces.ParsePolicy(json.RawMessage(
			`{"$type":"com.atproto.personalspace.policy","publicRead":true}`))
		require.NoError(t, err)
		require.Equal(t, spaces.Policy{Type: spaces.PolicyPersonalSpace}, p)
		p, err = spaces.ParsePolicy(
			json.RawMessage(`{"$type":"network.habitat.relationspace.policy"}`),
		)
		require.NoError(t, err)
		require.Equal(t, spaces.Policy{Type: spaces.PolicyRelationSpace}, p)
	})
}
