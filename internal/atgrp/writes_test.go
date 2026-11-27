package atgrp

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	atgrp_api "github.com/habitat-network/habitat/api/atgrp"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const memberAdminScope = "space:org.atgrp.members?skey=self&collection=org.atgrp.membership&collection=org.atgrp.role"

func canonicalMembers(t *testing.T) (*Store, spaces.Store) {
	t.Helper()
	s, repo := testGroupStore(t)
	policy := spaces.Policy{Type: spaces.PolicyAtgrp, Grants: []spaces.AtgrpGrant{
		{Role: MemberRole, Read: true, Write: true},
	}}
	require.NoError(
		t,
		spaces.SetGroupSpacePolicy(t.Context(), repo, MembersSpace(testGroup), policy),
	)
	return s, repo
}

func runGroupWrites(
	t *testing.T,
	s *Store,
	authorizer syntax.DID,
	scopes []string,
	writes ...spaces.Write,
) error {
	t.Helper()
	return runGroupWritesInSpace(t, s, MembersSpace(testGroup), authorizer, scopes, writes...)
}

func runGroupWritesInSpace(
	t *testing.T,
	s *Store,
	space habitat_syntax.SpaceURI,
	authorizer syntax.DID,
	scopes []string,
	writes ...spaces.Write,
) error {
	t.Helper()
	ctx := t.Context()
	return s.WithGroupTx(ctx, testGroup, func(tx *gorm.DB) error {
		group := s.WithTx(tx)
		plan, err := group.AuthorizeGroupRecordWrites(ctx, space, authorizer, scopes, writes)
		if err != nil {
			return err
		}
		if _, err := group.spaces.ApplyWrites(ctx, space, testGroup, writes); err != nil {
			return err
		}
		return group.ValidateGroupWriteResult(ctx, testGroup, plan)
	})
}

func roleWrite(t *testing.T, id string, role atgrp_api.OrgAtgrpRole) spaces.Write {
	t.Helper()
	return spaces.Write{
		Action: spaces.WriteUpdate, Collection: RoleCollection, Rkey: syntax.RecordKey(id),
		Value: spaces_testutil.MustMarshalRecord(t, role),
	}
}

func membershipWrite(t *testing.T, s *Store, did syntax.DID, roles ...string) spaces.Write {
	t.Helper()
	membership, err := s.GetMembership(t.Context(), testGroup, did)
	require.NoError(t, err)
	return spaces.Write{
		Action: spaces.WriteUpdate, Collection: MembershipCollection, Rkey: syntax.RecordKey(did),
		Value: spaces_testutil.MustMarshalRecord(t, atgrp_api.OrgAtgrpMembership{
			CreatedAt: membership.CreatedAt, Roles: append([]string{}, roles...),
		}),
	}
}

func TestRoleEditingCannotIncreaseOwnGrants(t *testing.T) {
	s, repo := canonicalMembers(t)
	putTestRole(t, repo, "manager", atgrp_api.OrgAtgrpRole{
		Permissions: []string{memberAdminScope}, CanAssign: []string{"manager", "helper"},
	})
	putTestRole(t, repo, "helper", atgrp_api.OrgAtgrpRole{})
	admit(t, s, member, "manager")
	cases := []struct {
		name string
		role atgrp_api.OrgAtgrpRole
	}{
		{
			name: "group-wide permission",
			role: atgrp_api.OrgAtgrpRole{
				Permissions: []string{memberAdminScope, "space:*?collection=*&manage=update"},
			},
		},
		{
			name: "expanded permission set",
			role: atgrp_api.OrgAtgrpRole{
				Permissions: []string{"include:org.atgrp.authManageAdmin"},
			},
		},
		{
			name: "unheld assignable role",
			role: atgrp_api.OrgAtgrpRole{CanAssign: []string{AdminRole}},
		},
		{
			name: "wildcard assignable role",
			role: atgrp_api.OrgAtgrpRole{CanAssign: []string{"*"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.ErrorIs(t, runGroupWrites(t, s, member, []string{memberAdminScope},
				roleWrite(t, "manager", tc.role)), ErrWriteForbidden)
		})
	}
	token := []string{memberAdminScope}
	require.ErrorIs(t, runGroupWrites(
		t,
		s,
		member,
		token,
		roleWrite(
			t,
			AdminRole,
			atgrp_api.OrgAtgrpRole{DisplayName: "Updated Admin"},
		),
	), ErrWriteForbidden)
	require.NoError(t, runGroupWrites(t, s, member, token,
		roleWrite(t, "helper", atgrp_api.OrgAtgrpRole{
			Permissions: []string{
				"space:org.atgrp.members?skey=self&collection=org.atgrp.role&action=update",
			},
			CanAssign: []string{"helper"},
		})))
}

func TestGroupWriteRequiresLiveActorAndIssuedPermission(t *testing.T) {
	s, repo := canonicalMembers(t)
	putTestRole(t, repo, "helper", atgrp_api.OrgAtgrpRole{})
	admit(t, s, member, AdminRole)
	write := roleWrite(t, "helper", atgrp_api.OrgAtgrpRole{DisplayName: "New name"})
	require.ErrorIs(
		t,
		runGroupWrites(t, s, "", []string{memberAdminScope}, write),
		ErrWriteForbidden,
	)
	require.ErrorIs(
		t,
		runGroupWrites(t, s, testGroup, []string{memberAdminScope}, write),
		ErrWriteForbidden,
	)
	require.ErrorIs(t, runGroupWrites(t, s, creator, []string{"atproto"}, write), ErrWriteForbidden)
	require.NoError(t, runGroupWrites(t, s, member, []string{memberAdminScope}, write))

	require.NoError(t, runGroupWrites(t, s, creator, []string{memberAdminScope},
		membershipWrite(t, s, member, MemberRole)))
	require.ErrorIs(
		t,
		runGroupWrites(t, s, member, []string{memberAdminScope}, write),
		ErrWriteForbidden,
	)
}

func TestGroupRecordGuardPreservesInviteOnlyAndLastAdmin(t *testing.T) {
	s, _ := canonicalMembers(t)
	token := []string{memberAdminScope}
	removal := membershipWrite(t, s, creator, MemberRole)
	require.ErrorIs(t, runGroupWrites(t, s, creator, token, removal), ErrLastAdmin)
	require.ErrorIs(t, runGroupWrites(t, s, creator, token,
		roleWrite(t, AdminRole, atgrp_api.OrgAtgrpRole{})), ErrLastAdmin)
	recovery, err := s.HasRecoveryMember(t.Context(), testGroup)
	require.NoError(t, err)
	require.True(t, recovery)

	for _, action := range []spaces.WriteAction{spaces.WriteCreate, spaces.WriteDelete} {
		direct := removal
		direct.Action = action
		require.ErrorIs(t, runGroupWrites(t, s, creator, token, direct), ErrWriteForbidden)
	}
}

func TestRoleCannotBeDeletedWhileReferenced(t *testing.T) {
	s, repo := canonicalMembers(t)
	putTestRole(t, repo, "helper", atgrp_api.OrgAtgrpRole{})
	admit(t, s, member, "helper")
	deleteRole := spaces.Write{
		Action:     spaces.WriteDelete,
		Collection: RoleCollection,
		Rkey:       "helper",
	}
	token := []string{memberAdminScope}
	require.ErrorIs(t, runGroupWrites(t, s, creator, token, deleteRole), ErrRoleInUse)

	ctx := t.Context()
	unlisted := habitat_syntax.ConstructSpaceURI(testGroup, "com.example.space", "private")
	_, err := repo.CreateSpace(ctx, testGroup, unlisted.SpaceType(), unlisted.Skey())
	require.NoError(t, err)
	policy := spaces.Policy{Type: spaces.PolicyAtgrp, Unlisted: true, Grants: []spaces.AtgrpGrant{
		{Role: "helper", Write: true},
	}}
	require.NoError(t, spaces.SetGroupSpacePolicy(ctx, repo, unlisted, policy))
	unassign := membershipWrite(t, s, member, MemberRole)
	require.ErrorIs(t, runGroupWrites(t, s, creator, token, deleteRole, unassign), ErrRoleInUse)
	unchanged, err := s.GetMembership(ctx, testGroup, member)
	require.NoError(t, err)
	require.Equal(t, []string{"helper"}, unchanged.Roles)

	policy.Grants = nil
	require.NoError(t, spaces.SetGroupSpacePolicy(ctx, repo, unlisted, policy))
	require.NoError(t, runGroupWrites(t, s, creator, token, deleteRole, unassign))
	_, err = s.getRole(ctx, testGroup, "helper")
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)
}

func TestRoleAndAssignmentBatchUsesOnePreWriteAuthorizer(t *testing.T) {
	s, repo := canonicalMembers(t)
	admit(t, s, member, MemberRole)
	create := roleWrite(t, "helper", atgrp_api.OrgAtgrpRole{})
	create.Action = spaces.WriteCreate
	assign := membershipWrite(t, s, member, MemberRole, "helper")
	token := []string{memberAdminScope}

	require.ErrorIs(t, runGroupWrites(t, s, creator, token, create, create), ErrInvalidRecord)
	require.ErrorIs(t, runGroupWrites(t, s, member, token, create, assign), ErrWriteForbidden)
	require.NoError(t, runGroupWrites(t, s, creator, token, create, assign))
	membership, err := s.GetMembership(t.Context(), testGroup, member)
	require.NoError(t, err)
	require.Equal(t, []string{MemberRole, "helper"}, membership.Roles)

	invalid := assign
	invalid.Value = spaces_testutil.MustMarshalRecord(t, atgrp_api.OrgAtgrpMembership{
		CreatedAt: "2000-01-01T00:00:00Z", Roles: []string{MemberRole},
	})
	require.ErrorIs(t, runGroupWrites(t, s, creator, token, invalid), ErrInvalidRecord)

	putTestRole(t, repo, "manager", atgrp_api.OrgAtgrpRole{
		Permissions: []string{memberAdminScope}, CanAssign: []string{"manager", "helper"},
	})
	other := syntax.DID("did:plc:another")
	admit(t, s, other, "manager")
	// The actor gives up their authority in the first write. The whole
	// batch was authorized before applying either write.
	revoke := membershipWrite(t, s, other)
	rename := roleWrite(t, "helper", atgrp_api.OrgAtgrpRole{DisplayName: "Helper"})
	require.NoError(t, runGroupWrites(t, s, other, token, revoke, rename))
	require.ErrorIs(t, runGroupWrites(t, s, other, token, rename), ErrWriteForbidden)
}

func TestDefaultRoleCannotDelegateHiddenPolicyAccess(t *testing.T) {
	s, repo := canonicalMembers(t)
	putTestRole(t, repo, "manager", atgrp_api.OrgAtgrpRole{
		Permissions: []string{memberAdminScope}, CanAssign: []string{"helper"},
	})
	putTestRole(t, repo, "helper", atgrp_api.OrgAtgrpRole{})
	admit(t, s, member, "manager")
	ctx := t.Context()
	space, err := repo.CreateSpace(ctx, testGroup, "com.example.space", "hidden")
	require.NoError(t, err)
	policy := spaces.Policy{Type: spaces.PolicyAtgrp, Unlisted: true, Grants: []spaces.AtgrpGrant{
		{Role: "helper", Read: true},
	}}
	require.NoError(t, spaces.SetGroupSpacePolicy(ctx, repo, space, policy))
	require.ErrorIs(t, runGroupWrites(t, s, member, []string{memberAdminScope},
		roleWrite(t, "helper", atgrp_api.OrgAtgrpRole{Default: true})), ErrWriteForbidden)
}

func TestConfirmationRequiresPersonalScopeAndCurrentMembership(t *testing.T) {
	s, _ := canonicalMembers(t)
	ctx := t.Context()
	admit(t, s, member, MemberRole)
	token := []string{"include:org.atgrp.authParticipate"}
	write := spaces.Write{
		Action: spaces.WriteCreate, Collection: ConfirmationCollection, Rkey: "self",
		Value: spaces_testutil.MustMarshalRecord(t, atgrp_api.OrgAtgrpConfirmation{
			CreatedAt: "2026-11-27T00:00:00Z",
		}),
	}
	space := MembersSpace(testGroup)
	require.NoError(
		t,
		s.AuthorizePersonalRecordWrites(ctx, space, member, token, []spaces.Write{write}),
	)
	require.ErrorIs(
		t,
		s.AuthorizePersonalRecordWrites(ctx, space, member, nil, []spaces.Write{write}),
		ErrWriteForbidden,
	)
	require.ErrorIs(
		t,
		s.AuthorizePersonalRecordWrites(ctx, space, testGroup, token, []spaces.Write{write}),
		ErrWriteForbidden,
	)
	wrongKey := write
	wrongKey.Rkey = "other"
	require.ErrorIs(
		t, s.AuthorizePersonalRecordWrites(ctx, space, member, token, []spaces.Write{wrongKey}),
		ErrInvalidRecord,
	)
	badDate := write
	badDate.Value = spaces_testutil.MustMarshalRecord(t, atgrp_api.OrgAtgrpConfirmation{
		CreatedAt: "not-a-date",
	})
	require.ErrorIs(
		t, s.AuthorizePersonalRecordWrites(ctx, space, member, token, []spaces.Write{badDate}),
		ErrInvalidRecord,
	)
	require.ErrorIs(
		t,
		runGroupWrites(t, s, creator, []string{adminSpacePermissions}, write),
		ErrWriteForbidden,
	)
	require.NoError(t, s.DeleteMembership(ctx, testGroup, member))
	require.ErrorIs(
		t,
		s.AuthorizePersonalRecordWrites(ctx, space, member, token, []spaces.Write{write}),
		ErrWriteForbidden,
	)
}

func TestGroupConfigStaysInviteOnlyInMeta(t *testing.T) {
	s, repo := canonicalMembers(t)
	ctx := t.Context()
	meta, err := repo.CreateSpace(ctx, testGroup, habitat_syntax.AtgrpMetaSpaceType, "self")
	require.NoError(t, err)
	require.NoError(
		t,
		spaces.SetGroupSpacePolicy(ctx, repo, meta, spaces.Policy{Type: spaces.PolicyAtgrp}),
	)
	scopes := []string{adminSpacePermissions}
	config := func(policy string) spaces.Write {
		return spaces.Write{
			Action:     spaces.WriteCreate,
			Collection: "org.atgrp.config",
			Rkey:       "self",
			Value: spaces_testutil.MustMarshalRecord(
				t,
				atgrp_api.OrgAtgrpConfig{JoinPolicy: policy},
			),
		}
	}
	for _, policy := range []string{"approval", "open", ""} {
		require.ErrorIs(
			t,
			runGroupWritesInSpace(t, s, meta, creator, scopes, config(policy)),
			ErrWriteForbidden,
		)
	}
	require.ErrorIs(t, runGroupWrites(t, s, creator, scopes, config("invite")), ErrWriteForbidden)
	require.NoError(t, runGroupWritesInSpace(t, s, meta, creator, scopes, config("invite")))
	remove := spaces.Write{Action: spaces.WriteDelete, Collection: "org.atgrp.config", Rkey: "self"}
	require.ErrorIs(
		t,
		runGroupWritesInSpace(t, s, meta, creator, scopes, remove),
		ErrWriteForbidden,
	)
	label := spaces.Write{
		Action:     spaces.WriteDelete,
		Collection: "com.atproto.label.record",
		Rkey:       "label",
	}
	require.ErrorIs(t, runGroupWritesInSpace(t, s, meta, creator, scopes, label), ErrWriteForbidden)
}
