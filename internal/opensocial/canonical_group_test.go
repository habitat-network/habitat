package opensocial_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	group_api "github.com/habitat-network/habitat/api/atgrp"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/hive"
	notify_testutil "github.com/habitat-network/habitat/internal/notify/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/opensocial/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestNewCanonicalGroup(t *testing.T) {
	ctx := t.Context()
	s := testutil.NewTestStore(t)
	creator := syntax.DID("did:plc:creator")
	group, err := s.NewCanonicalGroup(
		ctx,
		"workshop",
		creator,
		group_api.OrgAtgrpAbout{DisplayName: "Workshop"},
	)
	require.NoError(t, err)
	ident, err := s.Hive.LookupDID(ctx, group)
	require.NoError(t, err)
	require.Equal(t, syntax.Handle("workshop.example.com"), ident.Handle)
	canonical, err := s.IsCanonicalGroup(ctx, group)
	require.NoError(t, err)
	require.True(t, canonical)
	canonical, err = s.IsCanonicalGroup(ctx, "")
	require.NoError(t, err)
	require.False(t, canonical, "the lookup is exact even if the DID is empty")
	legacy, err := s.IsOrg(ctx, group)
	require.NoError(t, err)
	require.False(t, legacy)

	meta := habitat_syntax.ConstructSpaceURI(group, habitat_syntax.AtgrpMetaSpaceType, "self")
	members := habitat_syntax.ConstructSpaceURI(group, habitat_syntax.AtgrpMembersSpaceType, "self")
	for _, uri := range []habitat_syntax.SpaceURI{meta, members} {
		policy, err := s.SpaceStore.GetSpacePolicy(ctx, uri)
		require.NoError(t, err)
		require.Equal(t, spaces.PolicyAtgrp, policy.Type)
		require.False(t, policy.PublicRead)
		require.Contains(t, policy.Grants, spaces.AtgrpGrant{
			Role: "member", Read: true, Write: uri == members,
		})
		record, err := s.SpaceStore.GetRecord(ctx, uri, group, "com.atproto.space.policy", "self")
		require.NoError(t, err)
		require.Equal(t, "com.atproto.space.policy", record.Value["$type"])
		require.Equal(t, spaces.PolicyAtgrp, record.Value["policy"].(map[string]any)["$type"])
	}
	about, err := s.SpaceStore.GetRecord(ctx, meta, group, "org.atgrp.about", "self")
	require.NoError(t, err)
	require.Equal(t, "Workshop", about.Value["displayName"])
	config, err := s.SpaceStore.GetRecord(ctx, meta, group, "org.atgrp.config", "self")
	require.NoError(t, err)
	require.Equal(t, "invite", config.Value["joinPolicy"])
	admin, err := s.SpaceStore.GetRecord(ctx, members, group, "org.atgrp.role", "admin")
	require.NoError(t, err)
	require.Equal(t, []any{"*"}, admin.Value["canAssign"])
	member, err := s.SpaceStore.GetRecord(ctx, members, group, "org.atgrp.role", "member")
	require.NoError(t, err)
	require.Equal(t, true, member.Value["default"])
	membership, err := s.SpaceStore.GetRecord(
		ctx,
		members,
		group,
		"org.atgrp.membership",
		syntax.RecordKey(creator),
	)
	require.NoError(t, err)
	require.Contains(t, membership.Value["roles"], "admin")
	_, err = s.SpaceStore.GetRecord(ctx, members, creator, "org.atgrp.confirmation", "self")
	require.ErrorIs(t, err, spaces.ErrRecordNotFound, "the creator's PDS owns this record")
	_, err = s.NewCanonicalGroup(
		ctx,
		"workshop",
		creator,
		group_api.OrgAtgrpAbout{DisplayName: "Another group"},
	)
	require.ErrorIs(t, err, hive.ErrNotCreated)
}

func TestIsCanonicalGroupRejectsLegacyAndPartialGroups(t *testing.T) {
	ctx := t.Context()
	s := testutil.NewTestStore(t)
	creator := syntax.DID("did:plc:creator")
	legacy, err := s.NewOrg(ctx, "legacy", creator)
	require.NoError(t, err)
	did := syntax.DID(legacy)
	canonical, err := s.IsCanonicalGroup(ctx, did)
	require.NoError(t, err)
	require.False(t, canonical)

	meta, err := s.SpaceStore.CreateSpace(ctx, did, habitat_syntax.AtgrpMetaSpaceType, "self")
	require.NoError(t, err)
	require.NoError(
		t,
		spaces.SetGroupSpacePolicy(
			ctx,
			s.SpaceStore,
			meta,
			spaces.Policy{Type: spaces.PolicyAtgrp},
		),
	)
	members, err := s.SpaceStore.CreateSpace(ctx, did, habitat_syntax.AtgrpMembersSpaceType, "self")
	require.NoError(t, err)
	canonical, err = s.IsCanonicalGroup(ctx, did)
	require.NoError(t, err)
	require.False(t, canonical)
	require.NoError(
		t,
		spaces.SetGroupSpacePolicy(
			ctx,
			s.SpaceStore,
			members,
			spaces.Policy{Type: spaces.PolicyAtgrp},
		),
	)
	canonical, err = s.IsCanonicalGroup(ctx, did)
	require.NoError(t, err)
	require.False(t, canonical, "staging both reserved policies does not switch a legacy DID")

	foreign := syntax.DID("did:web:elsewhere.example")
	for _, spaceType := range []syntax.NSID{habitat_syntax.AtgrpMetaSpaceType, habitat_syntax.AtgrpMembersSpaceType} {
		uri, err := s.SpaceStore.CreateSpace(ctx, foreign, spaceType, "self")
		require.NoError(t, err)
		require.NoError(
			t,
			spaces.SetGroupSpacePolicy(
				ctx,
				s.SpaceStore,
				uri,
				spaces.Policy{Type: spaces.PolicyAtgrp},
			),
		)
	}
	canonical, err = s.IsCanonicalGroup(ctx, foreign)
	require.NoError(t, err)
	require.False(
		t,
		canonical,
		"locally stored space policies do not make this the authority's PDS",
	)
}

var errBootstrapWrite = errors.New("configured bootstrap write failure")

type failingGroupBootstrap struct {
	spaces.Store
	failedGroup *syntax.DID
}

func (s failingGroupBootstrap) WithTx(tx *gorm.DB) spaces.Store {
	s.Store = s.Store.WithTx(tx)
	return s
}

func (s failingGroupBootstrap) ApplyWrites(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	writes []spaces.Write,
) ([]spaces.WriteResult, error) {
	results, err := s.Store.ApplyWrites(ctx, space, repo, writes)
	if err == nil && space.SpaceType() == habitat_syntax.AtgrpMembersSpaceType {
		*s.failedGroup = repo
		return nil, errBootstrapWrite
	}
	return results, err
}

func TestNewCanonicalGroupRollsBack(t *testing.T) {
	ctx := t.Context()
	db := db_testutil.NewDB(t, opensocial.Models(), spaces.Models(), hive.Models())
	notifications := &notify_testutil.TestNotifier{}
	spaceStore := spaces_testutil.NewTestStore(
		t,
		spaces_testutil.WithDB(db),
		spaces_testutil.WithNotifier(notifications),
	)
	s := testutil.NewTestStore(t, testutil.WithDB(db), testutil.WithSpaceStore(spaceStore))
	var failedGroup syntax.DID
	failing, err := opensocial.NewStore(
		s.DB,
		failingGroupBootstrap{Store: s.SpaceStore, failedGroup: &failedGroup},
		s.BlobStore,
		s.Hive,
	)
	require.NoError(t, err)
	input := group_api.OrgAtgrpAbout{DisplayName: "Workshop"}
	creator := syntax.DID("did:plc:creator")
	_, err = failing.NewCanonicalGroup(ctx, "workshop", creator, input)
	require.ErrorIs(t, err, errBootstrapWrite)
	require.Empty(t, notifications.Writes, "observers must not hear about rolled back groups")
	_, err = s.Hive.LookupHandle(ctx, "workshop.example.com")
	require.ErrorIs(t, err, identity.ErrHandleNotFound)
	require.NotEmpty(t, failedGroup, "exercise the rollback after writing the roles and membership")
	_, err = s.Hive.LookupDID(ctx, failedGroup)
	require.ErrorIs(t, err, identity.ErrDIDNotFound)
	for _, spaceType := range []syntax.NSID{habitat_syntax.AtgrpMetaSpaceType, habitat_syntax.AtgrpMembersSpaceType} {
		_, err := s.SpaceStore.GetSpacePolicy(
			ctx,
			habitat_syntax.ConstructSpaceURI(failedGroup, spaceType, "self"),
		)
		require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
	}
	canonical, err := s.IsCanonicalGroup(ctx, failedGroup)
	require.NoError(t, err)
	require.False(t, canonical)

	group, err := s.NewCanonicalGroup(ctx, "workshop", creator, input)
	require.NoError(t, err)
	canonical, err = s.IsCanonicalGroup(ctx, group)
	require.NoError(t, err)
	require.True(t, canonical)
	require.NotEmpty(t, notifications.Writes, "committed records still notify observers")
}
