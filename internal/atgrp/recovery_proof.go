package atgrp

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"

	"github.com/habitat-network/habitat/internal/spaces"
)

// ErrFiniteRecoveryUnproven means an existing role cannot be shown to
// transfer recovery authority. It is not an authorization decision.
var ErrFiniteRecoveryUnproven = errors.New("finite group recovery is unproven")

// VerifyFiniteRecoveryRole checks a migration candidate against the approved
// role CID and exact finite assignments. The caller must prove source
// provenance and a real holder separately, from the same snapshot. This
// deliberately does not change native recovery or authorize any mutation.
func (s *Store) VerifyFiniteRecoveryRole(
	ctx context.Context, group syntax.DID, roleID string, expectedCID cid.Cid, assignable []string,
) error {
	if !expectedCID.Defined() || validateRoleIDs(assignable) != nil ||
		!slices.Contains(assignable, roleID) {
		return fmt.Errorf("%w: invalid finite recovery evidence", ErrFiniteRecoveryUnproven)
	}
	space := MembersSpace(group)
	record, err := s.spaces.GetRecord(ctx, space, group, RoleCollection, syntax.RecordKey(roleID))
	if err != nil {
		return fmt.Errorf("%w: read candidate recovery role: %w", ErrFiniteRecoveryUnproven, err)
	}
	// GetRecord selects the exact group-authored repo; its result does not
	// populate Owner.
	if !record.Cid.Equals(expectedCID) {
		return fmt.Errorf(
			"%w: candidate recovery role does not match pin",
			ErrFiniteRecoveryUnproven,
		)
	}
	role, err := readRole(record)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFiniteRecoveryUnproven, err)
	}
	if role.Default {
		return fmt.Errorf(
			"%w: recovery role must not be an admission default",
			ErrFiniteRecoveryUnproven,
		)
	}
	if !slices.Equal(
		slices.Sorted(slices.Values(role.CanAssign)),
		slices.Sorted(slices.Values(assignable)),
	) {
		return fmt.Errorf(
			"%w: candidate canAssign differs from finite source",
			ErrFiniteRecoveryUnproven,
		)
	}
	policy, err := s.spaces.GetSpacePolicy(ctx, space)
	if err != nil {
		return fmt.Errorf("%w: read members policy: %w", ErrFiniteRecoveryUnproven, err)
	}
	if policy.Type != spaces.PolicyAtgrp {
		return fmt.Errorf("%w: expected canonical members policy", ErrFiniteRecoveryUnproven)
	}
	// Evaluate this role alone, not the privileges a particular holder has
	// from a second role that would not transfer to the recipient.
	access, err := s.loadRoles(ctx, group, []string{roleID})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFiniteRecoveryUnproven, err)
	}
	live := access.SpacePermissions(space, policy)
	for _, required := range []struct {
		collection syntax.NSID
		action     string
	}{
		{RoleCollection, "read"},
		{RoleCollection, "update"},
		{MembershipCollection, "update"},
	} {
		if !live.AllowsRecord(space, required.collection, required.action) {
			return fmt.Errorf("%w: missing members %s %s", ErrFiniteRecoveryUnproven,
				required.collection, required.action)
		}
	}
	if !live.AllowsManage(space, "update") {
		return fmt.Errorf("%w: missing members policy manage=update", ErrFiniteRecoveryUnproven)
	}
	return nil
}
