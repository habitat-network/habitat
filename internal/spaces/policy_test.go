package spaces_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
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

func TestAtgrpPolicyParse(t *testing.T) {
	const valid = `{
		"$type":"org.atgrp.policy","publicRead":true,"unlisted":true,
		"grants":[{"role":"member","read":true,"write":true,"permissions":["space:*?manage=delete"]}]
	}`
	policy, err := spaces.ParsePolicy(json.RawMessage(valid))
	require.NoError(t, err)
	require.Equal(t, spaces.Policy{
		Type: spaces.PolicyAtgrp, PublicRead: true, Unlisted: true,
		Grants: []spaces.AtgrpGrant{{
			Role: "member", Read: true, Write: true, Permissions: []string{"space:*?manage=delete"},
		}},
	}, policy)
	raw, err := json.Marshal(policy)
	require.NoError(t, err)
	require.JSONEq(t, valid, string(raw))

	empty, err := json.Marshal(spaces.Policy{Type: spaces.PolicyAtgrp})
	require.NoError(t, err)
	require.JSONEq(t, `{"$type":"org.atgrp.policy","grants":[]}`, string(empty))

	for _, raw := range []string{
		`{"$type":"org.atgrp.policy"}`,
		`{"$type":"org.atgrp.policy","grants":null}`,
		`{"$type":"org.atgrp.policy","grants":[null]}`,
		`{"$type":"org.atgrp.policy","grants":[{"role":"a/b"}]}`,
		`{"$type":"org.atgrp.policy","grants":[{"role":"member"},{"role":"member"}]}`,
		`{"$type":"org.atgrp.policy","grants":[{"role":"member","permissions":[""]}]}`,
		`{"$type":"org.atgrp.policy","grants":[{"role":"member","permissions":["` + strings.Repeat("x", 2001) + `"]}]}`,
		`{"$type":"org.atgrp.policy","grants":[{"role":"member","permissions":[` + strings.Repeat(`"x",`, 100) + `"x"]}]}`,
		`{"$type":"org.atgrp.policy","grants":[` + strings.Repeat(`{"role":"member"},`, 100) + `{"role":"member"}]}`,
	} {
		_, err := spaces.ParsePolicy(json.RawMessage(raw))
		require.ErrorIs(t, err, spaces.ErrInvalidPolicy)
	}
}

func TestSetGroupSpacePolicy(t *testing.T) {
	ctx := t.Context()
	db := db_testutil.NewDB(t, spaces.Models())
	s := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(db))
	meta, err := s.CreateSpace(ctx, orgID, habitat_syntax.AtgrpMetaSpaceType, "self")
	require.NoError(t, err)
	members, err := s.CreateSpace(ctx, orgID, habitat_syntax.AtgrpMembersSpaceType, "self")
	require.NoError(t, err)
	modality, err := s.CreateSpace(ctx, orgID, syntax.NSID("com.example.modality"), "self")
	require.NoError(t, err)

	private := spaces.Policy{
		Type: spaces.PolicyAtgrp, Unlisted: true,
		Grants: []spaces.AtgrpGrant{{Role: "member", Read: true}},
	}
	require.ErrorIs(t, s.SetSpacePolicy(ctx, modality, private), spaces.ErrUnsupportedPolicy)
	for _, space := range []habitat_syntax.SpaceURI{meta, members, modality} {
		require.NoError(t, spaces.SetGroupSpacePolicy(ctx, s, space, private))
		policy, err := s.GetSpacePolicy(ctx, space)
		require.NoError(t, err)
		require.Equal(t, space == modality, policy.Unlisted)
		require.ErrorIs(
			t,
			s.SetSpacePolicy(ctx, space, spaces.DefaultPolicy()),
			spaces.ErrUnsupportedPolicy,
		)
		record, err := s.GetRecord(ctx, space, orgID, habitat_syntax.SpacePolicyCollection, "self")
		require.NoError(t, err)
		require.Equal(t, "com.atproto.space.policy", record.Value["$type"])
		raw, err := json.Marshal(record.Value["policy"])
		require.NoError(t, err)
		published, err := spaces.ParsePolicy(raw)
		require.NoError(t, err)
		require.Equal(t, policy, published)
	}

	rollback := errors.New("rollback policy update")
	err = spaces.Transaction(ctx, db, func(tx *gorm.DB) error {
		s := s.WithTx(tx)
		public := private
		public.PublicRead = true
		require.NoError(t, spaces.SetGroupSpacePolicy(ctx, s, modality, public))
		policy, err := s.GetSpacePolicy(ctx, modality)
		require.NoError(t, err)
		require.True(t, policy.PublicRead)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	policy, err := s.GetSpacePolicy(ctx, modality)
	require.NoError(t, err)
	require.Equal(t, private, policy)
	record, err := s.GetRecord(ctx, modality, orgID, habitat_syntax.SpacePolicyCollection, "self")
	require.NoError(t, err)
	require.NotContains(t, record.Value["policy"], "publicRead")

	require.ErrorIs(t, s.DeleteSpace(ctx, meta), spaces.ErrReservedSpace)
	require.ErrorIs(t, s.DeleteSpace(ctx, members), spaces.ErrReservedSpace)
	require.NoError(t, s.DeleteSpace(ctx, modality))
	for _, sibling := range []habitat_syntax.SpaceURI{meta, members} {
		_, err := s.GetSpacePolicy(ctx, sibling)
		require.NoError(t, err, "deleting a modality with skey=self must not delete siblings")
	}
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
