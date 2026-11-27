package opensocial

import (
	"context"
	"fmt"
	"slices"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/atgrp"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// StockRecoveryPins identifies the exact source and compiled role records
// retained by migration. A CID is a consistency check, not evidence of
// authorship or authority; the verifier reads them at their expected
// addresses and checks the source recovery semantics.
type StockRecoveryPins struct {
	Group              syntax.DID
	LegacyAdminRole    cid.Cid
	LegacyPermissions  cid.Cid
	CanonicalAdminRole cid.Cid
}

// VerifyStockRecovery checks recovery evidence for a disposable same-DID
// conversion of the stock legacy admin role. Use a transaction-bound store
// on the pinned snapshot. Success does not establish permission parity,
// enable the canonical group, or change the live last-admin predicate.
func (s *Store) VerifyStockRecovery(ctx context.Context, pins StockRecoveryPins) error {
	group := pins.Group
	if _, err := syntax.ParseDID(group.String()); err != nil {
		return fmt.Errorf("%w: invalid group DID", atgrp.ErrFiniteRecoveryUnproven)
	}
	if _, err := s.hive.LookupDID(ctx, group); err != nil {
		return fmt.Errorf(
			"%w: source group identity is not locally managed: %w",
			atgrp.ErrFiniteRecoveryUnproven,
			err,
		)
	}
	legacy := habitat_syntax.ConstructSpaceURI(group, MembersSpaceType, "self")
	role, err := s.readStockRecoveryRecord(
		ctx,
		legacy,
		RoleCollection,
		AdminRoleRkey,
		pins.LegacyAdminRole,
	)
	if err != nil {
		return err
	}
	var sourceRole opensocial_api.CommunityOpensocialRole
	if err := decodeRecordValue(role, &sourceRole); err != nil || sourceRole.Name == "" {
		return fmt.Errorf("%w: malformed legacy role", atgrp.ErrFiniteRecoveryUnproven)
	}
	source, err := s.readStockRecoveryRecord(
		ctx,
		legacy,
		PermissionsCollection,
		"self",
		pins.LegacyPermissions,
	)
	if err != nil {
		return err
	}
	var permissions opensocial_api.CommunityOpensocialPermissions
	if err := decodeRecordValue(source, &permissions); err != nil {
		return fmt.Errorf("%w: %w", atgrp.ErrFiniteRecoveryUnproven, err)
	}
	finite := []string{AdminRoleRkey, MemberRoleRkey}
	if !hasStockRecoveryBindings(permissions, finite) {
		return fmt.Errorf(
			"%w: source is not stock finite recovery",
			atgrp.ErrFiniteRecoveryUnproven,
		)
	}
	groups := atgrp.NewStore(s.db, s.spacesStore)
	if err := groups.VerifyFiniteRecoveryRole(
		ctx,
		group,
		atgrp.AdminRole,
		pins.CanonicalAdminRole,
		finite,
	); err != nil {
		return err
	}
	collection := syntax.NSID(atgrp.MembershipCollection)
	roster, err := s.spacesStore.ListRecords(ctx, atgrp.MembersSpace(group), group, &collection)
	if err != nil {
		return fmt.Errorf("%w: read candidate roster: %w", atgrp.ErrFiniteRecoveryUnproven, err)
	}
	hasHolder := false
	for _, record := range roster {
		holder := syntax.DID(record.Rkey)
		canonical, err := groups.GetMembership(ctx, group, holder)
		if err != nil {
			return fmt.Errorf(
				"%w: read candidate membership: %w",
				atgrp.ErrFiniteRecoveryUnproven,
				err,
			)
		}
		if !slices.Contains(canonical.Roles, atgrp.AdminRole) {
			continue
		}
		const legacyMembership = "community.opensocial.membership"
		sourceRecord, err := s.spacesStore.GetRecord(
			ctx,
			legacy,
			group,
			legacyMembership,
			record.Rkey,
		)
		if err != nil {
			return fmt.Errorf(
				"%w: read source membership: %w",
				atgrp.ErrFiniteRecoveryUnproven,
				err,
			)
		}
		var sourceMembership opensocial_api.CommunityOpensocialMembership
		if sourceRecord.Value["$type"] != legacyMembership ||
			decodeRecordValue(sourceRecord.Value, &sourceMembership) != nil ||
			!slices.Contains(sourceMembership.Roles, AdminRoleRkey) {
			return fmt.Errorf(
				"%w: candidate holder is not a source admin",
				atgrp.ErrFiniteRecoveryUnproven,
			)
		}
		hasHolder = true
	}
	if !hasHolder {
		return fmt.Errorf(
			"%w: no current holder of the recovery role",
			atgrp.ErrFiniteRecoveryUnproven,
		)
	}
	return nil
}

func (s *Store) readStockRecoveryRecord(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	collection syntax.NSID,
	key syntax.RecordKey,
	pin cid.Cid,
) (map[string]any, error) {
	record, err := s.spacesStore.GetRecord(ctx, space, space.SpaceOwner(), collection, key)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: read legacy %s: %w",
			atgrp.ErrFiniteRecoveryUnproven,
			collection,
			err,
		)
	}
	// The repo argument above binds authorship, independently of the pin.
	if !pin.Defined() || !record.Cid.Equals(pin) ||
		record.Value["$type"] != collection.String() {
		return nil, fmt.Errorf(
			"%w: legacy %s does not match source pin",
			atgrp.ErrFiniteRecoveryUnproven,
			collection,
		)
	}
	return record.Value, nil
}

func hasStockRecoveryBindings(
	permissions opensocial_api.CommunityOpensocialPermissions,
	finite []string,
) bool {
	// This proof covers the stock recovery-related bindings, not migration
	// of unrelated actions or permissions assigned to other roles.
	if len(permissions.Assignable) != 1 || permissions.Assignable[0].Role != AdminRoleRkey ||
		!slices.Equal(slices.Sorted(slices.Values(permissions.Assignable[0].Roles)), finite) {
		return false
	}
	for _, want := range []Action{ActionCommunityConfigure, ActionRoleAssign, ActionSpaceConfigure} {
		found := false
		for _, binding := range permissions.Bindings {
			if binding.Action != string(want) {
				continue
			}
			if found || !slices.Equal(binding.Roles, []string{AdminRoleRkey}) {
				return false
			}
			found = true
		}
		if !found {
			return false
		}
	}
	return true
}
