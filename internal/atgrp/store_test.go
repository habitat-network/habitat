package atgrp

import (
	"errors"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	atgrp_api "github.com/habitat-network/habitat/api/atgrp"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	notify_testutil "github.com/habitat-network/habitat/internal/notify/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	testGroup = syntax.DID("did:plc:testgroup")
	creator   = syntax.DID("did:plc:creator")
	member    = syntax.DID("did:plc:member")
)

func testGroupStore(t *testing.T) (*Store, spaces.Store) {
	t.Helper()
	return testGroupStoreWithNotifications(t, &notify_testutil.TestNotifier{})
}

func testGroupStoreWithNotifications(
	t *testing.T,
	notifications *notify_testutil.TestNotifier,
) (*Store, spaces.Store) {
	t.Helper()
	db := db_testutil.NewDB(t, spaces.Models())
	repo := spaces_testutil.NewTestStore(
		t,
		spaces_testutil.WithDB(db),
		spaces_testutil.WithNotifier(notifications),
	)
	_, err := repo.CreateSpace(t.Context(), testGroup, MembersSpaceType, "self")
	require.NoError(t, err)
	s := NewStore(db, repo)
	require.NoError(t, s.WithGroupTx(t.Context(), testGroup, func(tx *gorm.DB) error {
		return s.WithTx(tx).Bootstrap(t.Context(), testGroup, creator)
	}))
	return s, repo
}

func putTestRole(t *testing.T, repo spaces.Store, key string, role atgrp_api.OrgAtgrpRole) {
	t.Helper()
	_, _, err := repo.PutRecord(
		t.Context(), MembersSpace(testGroup), testGroup, RoleCollection, syntax.RecordKey(key),
		spaces_testutil.MustMarshalRecord(t, role),
	)
	require.NoError(t, err)
}

func admit(t *testing.T, s *Store, did syntax.DID, roles ...string) {
	t.Helper()
	require.NoError(t, s.WithGroupTx(t.Context(), testGroup, func(tx *gorm.DB) error {
		_, err := s.WithTx(tx).CreateMembership(t.Context(), testGroup, did, roles)
		return err
	}))
}

func TestStoreBootstrapAndConfirmation(t *testing.T) {
	s, repo := testGroupStore(t)
	ctx := t.Context()
	defaults, err := s.DefaultRoles(ctx, testGroup)
	require.NoError(t, err)
	require.Equal(t, []string{MemberRole}, defaults)

	created, err := s.GetMembership(ctx, testGroup, creator)
	require.NoError(t, err)
	require.Equal(t, []string{AdminRole, MemberRole}, created.Roles)
	require.NotEmpty(t, created.Ref.Cid)
	require.Equal(
		t,
		"at:space/did:plc:testgroup/org.atgrp.members/self/did:plc:testgroup/org.atgrp.membership/did:plc:creator",
		created.Ref.Uri,
	)
	access, err := s.LoadAccess(ctx, testGroup, creator)
	require.NoError(t, err)
	require.True(t, access.Member)
	require.True(t, access.CanAssignRole("created-later"))
	require.True(
		t,
		access.Permissions.AllowsRecord(MembersSpace(testGroup), RoleCollection, "update"),
	)
	confirmed, err := s.IsConfirmedMember(ctx, testGroup, creator)
	require.NoError(t, err)
	require.False(t, confirmed, "bootstrap must not author a member's confirmation")

	record := spaces_testutil.MustMarshalRecord(t, atgrp_api.OrgAtgrpConfirmation{
		CreatedAt: "2026-11-27T00:00:00Z",
	})
	_, _, err = repo.PutRecord(
		ctx,
		MembersSpace(testGroup),
		testGroup,
		ConfirmationCollection,
		"self",
		record,
	)
	require.NoError(t, err)
	confirmed, err = s.IsConfirmedMember(ctx, testGroup, creator)
	require.NoError(t, err)
	require.False(t, confirmed, "a confirmation must be authored by its member")

	_, _, err = repo.PutRecord(
		ctx,
		MembersSpace(testGroup),
		creator,
		ConfirmationCollection,
		"self",
		record,
	)
	require.NoError(t, err)
	confirmed, err = s.IsConfirmedMember(ctx, testGroup, creator)
	require.NoError(t, err)
	require.True(t, confirmed)

	_, _, err = repo.PutRecord(
		ctx,
		MembersSpace(testGroup),
		member,
		ConfirmationCollection,
		"self",
		record,
	)
	require.NoError(t, err)
	confirmed, err = s.IsConfirmedMember(ctx, testGroup, member)
	require.NoError(t, err)
	require.False(t, confirmed, "an unpaired confirmation must not appear in the roster")
	access, err = s.LoadAccess(ctx, testGroup, member)
	require.NoError(t, err)
	require.False(t, access.Member)
}

func TestCreateMembershipIsCreateOnlyAndTransactional(t *testing.T) {
	notifications := &notify_testutil.TestNotifier{}
	s, _ := testGroupStoreWithNotifications(t, notifications)
	ctx := t.Context()
	require.Len(t, notifications.Writes, 1, "bootstrap is one committed write")
	wantRollback := errors.New("reject later redemption write")
	err := s.WithGroupTx(ctx, testGroup, func(tx *gorm.DB) error {
		transaction := s.WithTx(tx)
		ref, err := transaction.CreateMembership(ctx, testGroup, member, nil)
		require.NoError(t, err)
		membership, err := transaction.GetMembership(ctx, testGroup, member)
		require.NoError(t, err)
		require.NotNil(t, membership.Roles)
		require.Empty(t, membership.Roles)
		require.Equal(t, ref, membership.Ref)
		require.Len(t, notifications.Writes, 1, "membership is not visible before the outer commit")
		return wantRollback
	})
	require.ErrorIs(t, err, wantRollback)
	require.Len(t, notifications.Writes, 1, "rolled-back membership is never announced")
	_, err = s.GetMembership(ctx, testGroup, member)
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)

	admit(t, s, member, MemberRole)
	require.Len(t, notifications.Writes, 2)
	before, err := s.GetMembership(ctx, testGroup, member)
	require.NoError(t, err)
	err = s.WithGroupTx(ctx, testGroup, func(tx *gorm.DB) error {
		_, err := s.WithTx(tx).CreateMembership(ctx, testGroup, member, []string{AdminRole})
		return err
	})
	require.ErrorIs(t, err, spaces.ErrRecordAlreadyExists)
	after, err := s.GetMembership(ctx, testGroup, member)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestLeaveProtectsEffectiveLastAdmin(t *testing.T) {
	s, repo := testGroupStore(t)
	ctx := t.Context()
	leave := func(did syntax.DID) error {
		return s.WithGroupTx(ctx, testGroup, func(tx *gorm.DB) error {
			return s.WithTx(tx).DeleteMembership(ctx, testGroup, did)
		})
	}
	require.ErrorIs(t, leave(creator), ErrLastAdmin)

	putTestRole(t, repo, "backup", atgrp_api.OrgAtgrpRole{
		CanAssign: []string{"*"},
	})
	require.NoError(t, spaces.SetGroupSpacePolicy(ctx, repo, MembersSpace(testGroup), spaces.Policy{
		Type: spaces.PolicyAtgrp,
		Grants: []spaces.AtgrpGrant{{
			Role: "backup", Permissions: []string{"include:org.atgrp.authManageAdmin"},
		}},
	}))
	admit(t, s, member, "backup")
	require.NoError(t, leave(creator))
	require.NoError(t, leave(creator))
	access, err := s.LoadAccess(ctx, testGroup, creator)
	require.NoError(t, err)
	require.False(t, access.Member)
	require.ErrorIs(t, leave(member), ErrLastAdmin)
}

func TestConcurrentLeavesKeepAnAdmin(t *testing.T) {
	s, _ := testGroupStore(t)
	admit(t, s, member, AdminRole)
	ctx := t.Context()
	results := make(chan error, 2)
	for _, did := range []syntax.DID{creator, member} {
		go func() {
			results <- s.WithGroupTx(ctx, testGroup, func(tx *gorm.DB) error {
				return s.WithTx(tx).DeleteMembership(ctx, testGroup, did)
			})
		}()
	}
	first, second := <-results, <-results
	if first == nil {
		require.ErrorIs(t, second, ErrLastAdmin)
	} else {
		require.ErrorIs(t, first, ErrLastAdmin)
		require.NoError(t, second)
	}
}

func TestScopeNarrowingUsesCurrentRoleAndPolicy(t *testing.T) {
	s, repo := testGroupStore(t)
	ctx := t.Context()
	space := habitat_syntax.ConstructSpaceURI(testGroup, "org.example.forum", "hidden")
	_, err := repo.CreateSpace(ctx, testGroup, space.SpaceType(), space.Skey())
	require.NoError(t, err)
	policy := spaces.Policy{
		Type:       spaces.PolicyAtgrp,
		Unlisted:   true,
		PublicRead: true,
		Grants: []spaces.AtgrpGrant{
			{
				Role: "writer", Write: true,
				Permissions: []string{"space:*?collection=org.example.post&action=update"},
			},
		},
	}
	require.NoError(t, spaces.SetGroupSpacePolicy(ctx, repo, space, policy))
	putTestRole(t, repo, "writer", atgrp_api.OrgAtgrpRole{CanAssign: []string{MemberRole}})
	admit(t, s, member, "writer")

	requested := []string{"atproto", "space:*?collection=*&action=create&action=update"}
	scopes, err := s.AuthorizeGroupScopes(ctx, testGroup, member, requested)
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{
			"atproto",
			"space:org.example.forum?action=update&collection=org.example.post&skey=hidden",
		},
		scopes,
	)
	access, err := s.LoadAccess(ctx, testGroup, member)
	require.NoError(t, err)
	require.True(t, access.CanChangeRoles(nil, []string{MemberRole}))
	require.False(t, access.CanChangeRoles([]string{AdminRole}, nil))
	require.False(t, access.CanAssignRole(wildcard))
	gotRead, gotWrite := access.PersonalSpaceAccess(space, policy)
	require.True(t, gotRead)
	require.True(t, gotWrite)

	require.NoError(
		t,
		repo.DeleteRecord(ctx, MembersSpace(testGroup), testGroup, RoleCollection, "writer"),
	)
	scopes, err = s.AuthorizeGroupScopes(ctx, testGroup, member, scopes)
	require.NoError(t, err)
	require.Equal(t, []string{"atproto"}, scopes)
	access, err = s.LoadAccess(ctx, testGroup, member)
	require.NoError(t, err)
	require.True(t, access.Member)
	require.Empty(t, access.Roles)
	gotRead, gotWrite = access.PersonalSpaceAccess(space, policy)
	require.True(t, gotRead)
	require.False(t, gotWrite)
}
