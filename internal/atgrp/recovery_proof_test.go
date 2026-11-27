package atgrp

import (
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	group_api "github.com/habitat-network/habitat/api/atgrp"
	"github.com/habitat-network/habitat/internal/spaces"
)

func TestVerifyFiniteRecoveryRole(t *testing.T) {
	s, repo := canonicalMembers(t)
	ctx := t.Context()
	space := MembersSpace(testGroup)
	finite := []string{AdminRole, MemberRole}
	edits := "space:org.atgrp.members?skey=self&collection=org.atgrp.role&collection=org.atgrp.membership&action=update"
	manage := "space:org.atgrp.members?skey=self&action=read&manage=update"
	candidate := group_api.OrgAtgrpRole{CanAssign: finite, Permissions: []string{edits}}
	putTestRole(t, repo, AdminRole, candidate)
	readPin := func() cid.Cid {
		t.Helper()
		record, err := repo.GetRecord(ctx, space, testGroup, RoleCollection, AdminRole)
		require.NoError(t, err)
		return record.Cid
	}
	pin := readPin()
	verify := func(pin cid.Cid, assignable []string) error {
		return s.VerifyFiniteRecoveryRole(ctx, testGroup, AdminRole, pin, assignable)
	}
	require.ErrorIs(t, verify(cid.Undef, finite), ErrFiniteRecoveryUnproven)
	require.ErrorIs(t, verify(pin, []string{MemberRole}), ErrFiniteRecoveryUnproven)
	require.ErrorIs(t, verify(pin, []string{"*"}), ErrFiniteRecoveryUnproven)
	require.ErrorContains(t, verify(pin, finite), "org.atgrp.role read")

	policy := spaces.Policy{Type: spaces.PolicyAtgrp, Grants: []spaces.AtgrpGrant{
		{Role: AdminRole, Permissions: []string{manage}},
	}}
	require.NoError(t, spaces.SetGroupSpacePolicy(ctx, repo, space, policy))
	require.NoError(t, verify(pin, []string{MemberRole, AdminRole}))
	candidate.Permissions = []string{
		"space:org.atgrp.members?skey=self&collection=org.atgrp.role&action=update",
	}
	putTestRole(t, repo, AdminRole, candidate)
	require.ErrorContains(t, verify(pin, finite), "does not match pin")
	require.ErrorContains(t, verify(readPin(), finite), "org.atgrp.membership update")
	candidate.Default = true
	putTestRole(t, repo, AdminRole, candidate)
	require.ErrorContains(t, verify(readPin(), finite), "must not be an admission default")

	candidate.Default = false
	candidate.CanAssign = []string{AdminRole, "unrelated"}
	putTestRole(t, repo, AdminRole, candidate)
	require.ErrorContains(t, verify(readPin(), finite), "differs from finite source")
}
