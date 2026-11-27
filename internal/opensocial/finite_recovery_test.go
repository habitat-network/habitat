package opensocial_test

import (
	"database/sql"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	group_api "github.com/habitat-network/habitat/api/atgrp"
	legacy_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/atgrp"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/opensocial/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	recoveryRead    = "space:org.atgrp.members?skey=self&action=read"
	recoveryEdits   = "space:org.atgrp.members?skey=self&collection=org.atgrp.role&collection=org.atgrp.membership&action=update"
	recoveryManage  = recoveryRead + "&manage=update"
	recoveryCreator = syntax.DID("did:plc:recoverycreator")
	recoveryMember  = syntax.DID("did:plc:recoverymember")
)

type finiteFixture struct {
	*testutil.TestStore
	pins   opensocial.StockRecoveryPins
	legacy habitat_syntax.SpaceURI
	target habitat_syntax.SpaceURI
	policy spaces.Policy
	role   group_api.OrgAtgrpRole
}

func newFiniteFixture(t *testing.T, scopes ...string) *finiteFixture {
	t.Helper()
	ctx := t.Context()
	store := testutil.NewTestStore(t)
	id, err := store.NewOrg(ctx, "finite", recoveryCreator)
	require.NoError(t, err)
	group := syntax.DID(id)
	require.NoError(t, store.ProvisionMember(ctx, group, recoveryMember))
	require.NoError(t, store.PutRole(ctx, group, "unrelated", "Unrelated", ""))
	require.NoError(
		t,
		store.AssignRoles(ctx, group, recoveryMember, []string{"unrelated", atgrp.MemberRole}),
	)
	legacy := habitat_syntax.ConstructSpaceURI(group, opensocial.MembersSpaceType, "self")
	role, err := store.SpaceStore.GetRecord(
		ctx,
		legacy,
		group,
		opensocial.RoleCollection,
		atgrp.AdminRole,
	)
	require.NoError(t, err)
	permissions, err := store.SpaceStore.GetRecord(
		ctx,
		legacy,
		group,
		opensocial.PermissionsCollection,
		"self",
	)
	require.NoError(t, err)

	f := &finiteFixture{
		TestStore: store, legacy: legacy, target: atgrp.MembersSpace(group),
		pins: opensocial.StockRecoveryPins{
			Group: group, LegacyAdminRole: role.Cid, LegacyPermissions: permissions.Cid,
		},
		// These are candidate grants in a disposable fixture, not a mapping
		// of legacy action bindings to canonical permissions.
		role: group_api.OrgAtgrpRole{
			DisplayName: "Admin",
			CanAssign:   []string{atgrp.AdminRole, atgrp.MemberRole},
			Permissions: scopes,
		},
		policy: spaces.Policy{Type: spaces.PolicyAtgrp, Grants: []spaces.AtgrpGrant{
			{Role: atgrp.AdminRole, Read: true, Write: true},
			{Role: atgrp.MemberRole, Read: true, Write: true},
		}},
	}
	_, err = store.SpaceStore.CreateSpace(ctx, group, atgrp.MembersSpaceType, "self")
	require.NoError(t, err)
	require.NoError(t, spaces.SetGroupSpacePolicy(ctx, store.SpaceStore, f.target, f.policy))
	f.pins.CanonicalAdminRole = f.writeCandidateRole(t, atgrp.AdminRole, f.role)
	f.writeCandidateRole(t, atgrp.MemberRole, group_api.OrgAtgrpRole{})
	f.writeCandidateRole(t, "unrelated", group_api.OrgAtgrpRole{})
	groups := atgrp.NewStore(store.DB, store.SpaceStore)
	for _, holder := range []syntax.DID{recoveryCreator, recoveryMember} {
		roles, err := store.GetUserRoles(ctx, group, holder)
		require.NoError(t, err)
		_, err = groups.CreateMembership(ctx, group, holder, roles)
		require.NoError(t, err)
	}
	return f
}

func (f *finiteFixture) writeCandidateRole(
	t *testing.T,
	id string,
	role group_api.OrgAtgrpRole,
) cid.Cid {
	t.Helper()
	_, value, err := f.SpaceStore.PutRecord(
		t.Context(),
		f.target,
		f.pins.Group,
		atgrp.RoleCollection,
		syntax.RecordKey(id),
		spaces_testutil.MustMarshalRecord(t, role),
	)
	require.NoError(t, err)
	return *value
}

func (f *finiteFixture) verify(t *testing.T, pins opensocial.StockRecoveryPins) error {
	t.Helper()
	return f.DB.WithContext(t.Context()).Transaction(func(tx *gorm.DB) error {
		return f.WithTx(tx).VerifyStockRecovery(t.Context(), pins)
	}, &sql.TxOptions{ReadOnly: true})
}

func TestStockFiniteRecoveryTransferAndRepair(t *testing.T) {
	f := newFiniteFixture(t, recoveryEdits, recoveryManage)
	ctx, group := t.Context(), f.pins.Group
	groups := atgrp.NewStore(f.DB, f.SpaceStore)
	before, err := f.SpaceStore.SpaceRev(ctx, f.target)
	require.NoError(t, err)
	require.NoError(t, f.verify(t, f.pins))
	after, err := f.SpaceStore.SpaceRev(ctx, f.target)
	require.NoError(t, err)
	require.Equal(t, before, after, "proof does not write")
	native, err := groups.HasRecoveryMember(ctx, group)
	require.NoError(t, err)
	require.False(t, native, "finite evidence must not change native recovery")

	member, err := groups.GetMembership(ctx, group, recoveryMember)
	require.NoError(t, err)
	next := append(append([]string{}, member.Roles...), atgrp.AdminRole)
	allowed, err := f.CanAssignRoles(ctx, group, recoveryCreator, member.Roles, next)
	require.NoError(t, err)
	require.True(t, allowed, "legacy stock role permits the same promotion")

	require.NoError(t, groups.WithGroupTx(ctx, group, func(tx *gorm.DB) error {
		current := groups.WithTx(tx)
		repo := f.SpaceStore.WithTx(tx)
		// Use the ordinary token ceiling and live guard, not a recovery
		// credential or a test-only exemption in the guard.
		issue := func(actor syntax.DID) []string {
			t.Helper()
			issued, err := current.AuthorizeGroupScopes(
				ctx,
				group,
				actor,
				[]string{recoveryEdits, recoveryManage},
			)
			require.NoError(t, err)
			return issued
		}
		apply := func(actor syntax.DID, scopes []string, write spaces.Write) error {
			plan, err := current.AuthorizeGroupRecordWrites(
				ctx,
				f.target,
				actor,
				scopes,
				[]spaces.Write{write},
			)
			if err != nil {
				return err
			}
			if _, err := repo.ApplyWrites(ctx, f.target, group, []spaces.Write{write}); err != nil {
				return err
			}
			return current.ValidateGroupWriteResult(ctx, group, plan)
		}
		promotion := spaces.Write{
			Action:     spaces.WriteUpdate,
			Collection: atgrp.MembershipCollection,
			Rkey:       syntax.RecordKey(recoveryMember),
			Value: spaces_testutil.MustMarshalRecord(t, group_api.OrgAtgrpMembership{
				CreatedAt: member.CreatedAt, Roles: next,
			}),
		}
		require.ErrorIs(t, apply(recoveryCreator, nil, promotion), atgrp.ErrWriteForbidden)
		require.NoError(t, apply(recoveryCreator, issue(recoveryCreator), promotion))
		promoted, err := current.GetMembership(ctx, group, recoveryMember)
		require.NoError(t, err)
		require.Equal(t, member.CreatedAt, promoted.CreatedAt)
		require.Equal(t, next, promoted.Roles)
		scopes := issue(recoveryMember)

		// Move a held permission into the role as a repair, with no new
		// effective authority. Then use the same guard to change policy.
		role := f.role
		role.DisplayName = "Recovery administrator"
		role.Permissions = append(append([]string{}, role.Permissions...), recoveryRead)
		update := spaces.Write{
			Action:     spaces.WriteUpdate,
			Collection: atgrp.RoleCollection,
			Rkey:       atgrp.AdminRole,
			Value:      spaces_testutil.MustMarshalRecord(t, role),
		}
		require.NoError(t, apply(recoveryMember, scopes, update))
		actor, err := current.LoadAccess(ctx, group, recoveryMember)
		require.NoError(t, err)
		require.True(t, atgrp.ParsePermissions(group, scopes).AllowsManage(f.target, "update"))
		require.True(t, actor.SpacePermissions(f.target, f.policy).AllowsManage(f.target, "update"))
		policy := f.policy
		policy.Grants = append([]spaces.AtgrpGrant{}, policy.Grants...)
		policy.Grants[0].Permissions = []string{recoveryRead}
		require.NoError(t, current.ValidatePolicyChange(ctx, f.target, actor, f.policy, policy))
		require.NoError(t, spaces.SetGroupSpacePolicy(ctx, repo, f.target, policy))
		broadened := policy
		broadened.Grants = append([]spaces.AtgrpGrant{}, policy.Grants...)
		broadened.Grants[0].Permissions = []string{
			"space:org.atgrp.members?skey=self&collection=org.atgrp.membership&action=delete",
		}
		require.ErrorIs(t, current.ValidatePolicyChange(ctx, f.target, actor, policy, broadened),
			atgrp.ErrWriteForbidden)

		update.Rkey = "unrelated"
		require.ErrorIs(t, apply(recoveryMember, scopes, update), atgrp.ErrWriteForbidden)
		update.Rkey = atgrp.AdminRole
		role.CanAssign = []string{"*"}
		update.Value = spaces_testutil.MustMarshalRecord(t, role)
		require.ErrorIs(t, apply(recoveryMember, scopes, update), atgrp.ErrWriteForbidden)
		require.False(t, actor.CanAssignRole("created-later"))
		return nil
	}))
}

func TestStockFiniteRecoveryRejectsMissingAuthorityAndProvenance(t *testing.T) {
	t.Run("no policy repair authority even with stock legacy bindings", func(t *testing.T) {
		f := newFiniteFixture(t, recoveryRead, recoveryEdits)
		err := f.verify(t, f.pins)
		require.ErrorIs(t, err, atgrp.ErrFiniteRecoveryUnproven)
		require.ErrorContains(t, err, "missing members policy manage=update")

		// The holder also has the member role, but recovery must transfer
		// by assigning only admin, without relying on another held role.
		f.writeCandidateRole(
			t,
			atgrp.MemberRole,
			group_api.OrgAtgrpRole{Permissions: []string{recoveryManage}},
		)
		require.ErrorContains(t, f.verify(t, f.pins), "missing members policy manage=update")
	})
	t.Run("changed CID and wrong group", func(t *testing.T) {
		f := newFiniteFixture(t, recoveryEdits, recoveryManage)
		pins := f.pins
		pins.LegacyAdminRole = pins.CanonicalAdminRole
		require.ErrorContains(t, f.verify(t, pins), "does not match source pin")
		pins = f.pins
		pins.CanonicalAdminRole = cid.Undef
		require.ErrorIs(t, f.verify(t, pins), atgrp.ErrFiniteRecoveryUnproven)
		pins = f.pins
		pins.Group = syntax.DID("did:plc:othergroup")
		require.ErrorIs(t, f.verify(t, pins), atgrp.ErrFiniteRecoveryUnproven)
	})
	t.Run("re-pinning explicit wildcard is not proof", func(t *testing.T) {
		f := newFiniteFixture(t, recoveryEdits, recoveryManage)
		f.role.CanAssign = []string{"*"}
		f.pins.CanonicalAdminRole = f.writeCandidateRole(t, atgrp.AdminRole, f.role)
		require.ErrorContains(t, f.verify(t, f.pins), "differs from finite source")
	})
	t.Run("freshly pinned fake source grants do not establish provenance", func(t *testing.T) {
		f := newFiniteFixture(t, recoveryEdits, recoveryManage)
		permissions := legacy_api.CommunityOpensocialPermissions{
			Assignable: []legacy_api.CommunityOpensocialPermissionsAssignableBinding{
				{Role: atgrp.AdminRole, Roles: []string{atgrp.AdminRole, atgrp.MemberRole}},
			},
		}
		_, pin, err := f.SpaceStore.PutRecord(
			t.Context(),
			f.legacy,
			f.pins.Group,
			opensocial.PermissionsCollection,
			"self",
			spaces_testutil.MustMarshalRecord(t, permissions),
		)
		require.NoError(t, err)
		f.pins.LegacyPermissions = *pin
		require.ErrorContains(t, f.verify(t, f.pins), "source is not stock finite recovery")
	})
	t.Run("no source holder", func(t *testing.T) {
		f := newFiniteFixture(t, recoveryEdits, recoveryManage)
		require.NoError(
			t,
			f.AssignRoles(t.Context(), f.pins.Group, recoveryCreator, []string{atgrp.MemberRole}),
		)
		require.ErrorContains(t, f.verify(t, f.pins), "candidate holder is not a source admin")
	})
	t.Run("no canonical holder", func(t *testing.T) {
		f := newFiniteFixture(t, recoveryEdits, recoveryManage)
		groups := atgrp.NewStore(f.DB, f.SpaceStore)
		require.NoError(t, groups.DeleteMembership(t.Context(), f.pins.Group, recoveryCreator))
		require.ErrorContains(t, f.verify(t, f.pins), "no current holder")
	})
	t.Run("another repo cannot supply the pinned legacy role", func(t *testing.T) {
		f := newFiniteFixture(t, recoveryEdits, recoveryManage)
		ctx := t.Context()
		record, err := f.SpaceStore.GetRecord(
			ctx,
			f.legacy,
			f.pins.Group,
			opensocial.RoleCollection,
			atgrp.AdminRole,
		)
		require.NoError(t, err)
		bytes, err := spaces.MarshalRecord(record.Value)
		require.NoError(t, err)
		_, other, err := f.SpaceStore.PutRecord(
			ctx,
			f.legacy,
			recoveryCreator,
			opensocial.RoleCollection,
			atgrp.AdminRole,
			bytes,
		)
		require.NoError(t, err)
		require.Equal(t, f.pins.LegacyAdminRole, *other)
		require.NoError(
			t,
			f.SpaceStore.DeleteRecord(
				ctx,
				f.legacy,
				f.pins.Group,
				opensocial.RoleCollection,
				atgrp.AdminRole,
			),
		)
		require.ErrorIs(t, f.verify(t, f.pins), spaces.ErrRecordNotFound)
	})
}
