package opensocial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// The names groups used before Habitat followed the org.atgrp spec.
const (
	legacyMembersSpaceType = "community.opensocial.members"
	legacyAboutSpaceType   = "community.opensocial.about"

	legacyProfileCollection     = "community.opensocial.profile"
	legacyRoleCollection        = "community.opensocial.role"
	legacyMembershipCollection  = "community.opensocial.membership"
	legacyAcceptanceCollection  = "community.opensocial.acceptance"
	legacyMemberProfile         = "community.opensocial.memberProfile"
	legacyPermissionsCollection = "community.opensocial.permissions"
	legacyAccessCollection      = "community.opensocial.access"
	legacySpaceCollection       = "community.opensocial.space"
)

// MigrateLegacyGroups rewrites every group stored under the community.opensocial
// names as an org.atgrp group, in place, one group per transaction:
//
//   - the about space's profile becomes the meta space's about record, and the
//     group is given a config with the invite-only join policy it always had;
//   - the members space becomes org.atgrp.members: roles carry the scopes of
//     the actions the permissions record bound them to and the roles they could
//     assign, memberships and member profiles move across, and each member's
//     acceptance becomes their confirmation;
//   - every other space's access record becomes the space's policy, with the
//     roles it named reading and writing, and the space index is rebuilt from
//     the policies (a space the old index did not list becomes unlisted);
//   - pending invite rows become invite records in the invitee's inbox.
//
// Groups whose members space already has the new name are skipped, so it is
// safe to run again.
func (s *Store) MigrateLegacyGroups(ctx context.Context) error {
	return MigrateLegacyGroups(ctx, s.db, s.spacesStore)
}

// MigrateLegacyGroups is [Store.MigrateLegacyGroups] for callers, such as a
// database migration, that have a database and spaces store but no full Store.
func MigrateLegacyGroups(ctx context.Context, db *gorm.DB, spacesStore spaces.Store) error {
	s := &Store{db: db, spacesStore: spacesStore}
	var owners []syntax.DID
	if err := s.db.WithContext(ctx).Table("spaces").
		Where("type = ?", legacyMembersSpaceType).Pluck("owner", &owners).Error; err != nil {
		return fmt.Errorf("list legacy groups: %w", err)
	}
	for _, org := range owners {
		if err := s.migrateLegacyGroup(ctx, org); err != nil {
			return fmt.Errorf("migrate group %s: %w", org, err)
		}
	}
	return nil
}

func (s *Store) migrateLegacyGroup(ctx context.Context, org syntax.DID) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Not WithTx: the migration needs neither the blob store nor the hive.
		scoped := &Store{db: tx, spacesStore: s.spacesStore.WithTx(tx)}
		return scoped.migrateLegacyGroupTx(ctx, org)
	})
}

func (s *Store) migrateLegacyGroupTx(ctx context.Context, org syntax.DID) error {
	oldMembers := habitat_syntax.ConstructSpaceURI(org, legacyMembersSpaceType, "self")
	oldAbout := habitat_syntax.ConstructSpaceURI(org, legacyAboutSpaceType, "self")
	store := s.spacesStore

	collection := func(space habitat_syntax.SpaceURI, name string) ([]spaces.Record, error) {
		records, err := store.ListCollection(ctx, space, syntax.NSID(name))
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", name, err)
		}
		return records, nil
	}
	put := func(
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
		name string,
		rkey syntax.RecordKey,
		value map[string]any,
	) error {
		value["$type"] = name
		raw, err := spaces.MarshalRecord(value)
		if err != nil {
			return fmt.Errorf("marshal %s: %w", name, err)
		}
		_, _, err = store.PutRecord(ctx, space, repo, syntax.NSID(name), rkey, raw)
		if err != nil {
			return fmt.Errorf("put %s: %w", name, err)
		}
		return nil
	}

	metaSpace, err := store.CreateSpace(ctx, org, MetaSpaceType, "self")
	if err != nil {
		return fmt.Errorf("create meta space: %w", err)
	}
	membersSpace, err := store.CreateSpace(ctx, org, MembersSpaceType, "self")
	if err != nil {
		return fmt.Errorf("create members space: %w", err)
	}

	// About and config.
	about := map[string]any{}
	if profile, err := store.GetRecord(
		ctx, oldAbout, org, legacyProfileCollection, "self",
	); err == nil {
		if name, ok := profile.Value["name"]; ok {
			about["displayName"] = name
		}
		for _, key := range []string{"description", "avatar"} {
			if v, ok := profile.Value[key]; ok {
				about[key] = v
			}
		}
	} else if !errors.Is(err, spaces.ErrRecordNotFound) {
		return fmt.Errorf("get profile: %w", err)
	}
	if err := put(metaSpace, org, AboutCollection, "self", about); err != nil {
		return err
	}
	if err := put(
		metaSpace, org, ConfigCollection, "self",
		map[string]any{"joinPolicy": JoinPolicyInvite},
	); err != nil {
		return err
	}

	// Roles, from the old role and permissions records.
	oldRoles, err := collection(oldMembers, legacyRoleCollection)
	if err != nil {
		return err
	}
	permissions, err := s.legacyPermissions(ctx, oldMembers, org)
	if err != nil {
		return err
	}
	declared := map[string]bool{}
	for _, record := range oldRoles {
		id := string(record.Rkey)
		declared[id] = true
		role := map[string]any{}
		if v, ok := record.Value["name"]; ok {
			role["displayName"] = v
		}
		if v, ok := record.Value["description"]; ok {
			role["description"] = v
		}
		if id == MemberRoleRkey {
			role["default"] = true
		}
		role["permissions"] = permissions.scopes(id)
		role["canAssign"] = permissions.canAssign(id)
		if err := put(membersSpace, org, RoleCollection, record.Rkey, role); err != nil {
			return err
		}
	}

	// Memberships, confirmations and member profiles.
	for _, move := range []struct {
		from, to string
		convert  func(map[string]any) map[string]any
	}{
		{legacyMembershipCollection, MembershipCollection, func(v map[string]any) map[string]any {
			out := map[string]any{"roles": v["roles"]}
			if at, ok := v["updatedAt"]; ok {
				out["createdAt"] = at
			}
			return out
		}},
		{legacyAcceptanceCollection, ConfirmationCollection, func(v map[string]any) map[string]any {
			out := map[string]any{}
			if at, ok := v["updatedAt"]; ok {
				out["createdAt"] = at
			}
			return out
		}},
		{legacyMemberProfile, MemberProfileCollection, maps.Clone[map[string]any]},
	} {
		records, err := collection(oldMembers, move.from)
		if err != nil {
			return err
		}
		for _, record := range records {
			if err := put(
				membersSpace, record.Owner, move.to, record.Rkey, move.convert(record.Value),
			); err != nil {
				return err
			}
		}
	}

	// Policies. The built-in spaces get the bootstrap policies.
	if err := s.SetPolicy(ctx, metaSpace, bootstrapPolicy(true, false)); err != nil {
		return err
	}
	if err := s.SetPolicy(ctx, membersSpace, bootstrapPolicy(false, true)); err != nil {
		return err
	}
	listed := map[string]bool{}
	index, err := collection(oldMembers, legacySpaceCollection)
	if err != nil {
		return err
	}
	for _, entry := range index {
		uri, _ := entry.Value["uri"].(string)
		listed[habitat_syntax.SpaceURI(uri).Canonical().String()] = true
	}
	var rows []struct {
		Type syntax.NSID
		Skey habitat_syntax.SpaceKey
	}
	if err := s.db.Table("spaces").Select("type", "skey").
		Where("owner = ?", org).Find(&rows).Error; err != nil {
		return fmt.Errorf("list group spaces: %w", err)
	}
	for _, row := range rows {
		if isReservedSpaceType(row.Type) || isLegacySpaceType(row.Type) {
			continue
		}
		space := habitat_syntax.ConstructSpaceURI(org, row.Type, row.Skey)
		access, err := store.GetRecord(ctx, space, org, legacyAccessCollection, "self")
		if errors.Is(err, spaces.ErrRecordNotFound) {
			continue
		} else if err != nil {
			return fmt.Errorf("get access of %s: %w", space, err)
		}
		policy := spaces.Policy{
			Type:     spaces.PolicyGroupSpace,
			Unlisted: !listed[space.Canonical().String()],
		}
		roles, _ := access.Value["roles"].([]any)
		for _, r := range roles {
			if id, ok := r.(string); ok && declared[id] {
				policy.Grants = append(
					policy.Grants,
					spaces.GroupGrant{Role: id, Read: true, Write: true},
				)
			}
		}
		if err := s.SetPolicy(ctx, space, policy); err != nil {
			return fmt.Errorf("set policy of %s: %w", space, err)
		}
		if err := store.DeleteRecord(ctx, space, org, legacyAccessCollection, "self"); err != nil {
			return fmt.Errorf("delete access of %s: %w", space, err)
		}
	}

	if err := s.migrateLegacyInvites(ctx, org); err != nil {
		return err
	}
	for _, old := range []habitat_syntax.SpaceURI{oldMembers, oldAbout} {
		if err := store.DeleteSpace(ctx, old); err != nil &&
			!errors.Is(err, spaces.ErrSpaceNotFound) {
			return fmt.Errorf("delete %s: %w", old, err)
		}
	}
	return nil
}

func isLegacySpaceType(t syntax.NSID) bool {
	return t == legacyMembersSpaceType || t == legacyAboutSpaceType
}

// migrateLegacyInvites turns org's rows of the old invites table into invite
// records in each invitee's inbox, then deletes the rows.
func (s *Store) migrateLegacyInvites(ctx context.Context, org syntax.DID) error {
	var rows []inviteRow
	if err := s.db.Where("org_id = ?", org).Find(&rows).Error; err != nil {
		return fmt.Errorf("list legacy invites: %w", err)
	}
	for _, row := range rows {
		var roles []string
		if err := json.Unmarshal([]byte(row.Roles), &roles); err != nil {
			return fmt.Errorf("decode roles of invite %s: %w", row.ID, err)
		}
		inbox := invitesSpace(row.Invitee)
		_, err := s.spacesStore.CreateSpace(ctx, row.Invitee, InboxSpaceType, "self")
		if err != nil && !errors.Is(err, spaces.ErrSpaceAlreadyExists) {
			return fmt.Errorf("create inbox of %s: %w", row.Invitee, err)
		}
		raw, err := spaces.MarshalRecord(map[string]any{
			"$type":     InviteCollection,
			"createdAt": row.CreatedAt.UTC().Format(time.RFC3339),
			"roles":     roles,
		})
		if err != nil {
			return fmt.Errorf("marshal invite %s: %w", row.ID, err)
		}
		if _, _, err := s.spacesStore.PutRecord(
			ctx, inbox, org, InviteCollection, syntax.RecordKey(row.ID), raw,
		); err != nil {
			return fmt.Errorf("put invite %s: %w", row.ID, err)
		}
		if err := s.db.Delete(&row).Error; err != nil {
			return fmt.Errorf("delete legacy invite %s: %w", row.ID, err)
		}
	}
	return nil
}

// legacyPermissionSet is the old permissions record: which roles each action
// was bound to, and which roles each role could assign.
type legacyPermissionSet struct {
	bound      map[Action][]string
	assignable map[string][]string
	configured bool
}

func (s *Store) legacyPermissions(
	ctx context.Context,
	members habitat_syntax.SpaceURI,
	org syntax.DID,
) (legacyPermissionSet, error) {
	set := legacyPermissionSet{bound: map[Action][]string{}, assignable: map[string][]string{}}
	record, err := s.spacesStore.GetRecord(ctx, members, org, legacyPermissionsCollection, "self")
	if errors.Is(err, spaces.ErrRecordNotFound) {
		// Without a record only admins could act, and could assign any role.
		for _, action := range AllActions {
			set.bound[action] = []string{AdminRoleRkey}
		}
		set.assignable[AdminRoleRkey] = []string{AssignAll}
		return set, nil
	} else if err != nil {
		return set, fmt.Errorf("get permissions: %w", err)
	}
	var old struct {
		Bindings []struct {
			Action string   `json:"action"`
			Roles  []string `json:"roles"`
		} `json:"bindings"`
		Assignable []struct {
			Role  string   `json:"role"`
			Roles []string `json:"roles"`
		} `json:"assignable"`
	}
	if err := decodeRecordValue(record.Value, &old); err != nil {
		return set, fmt.Errorf("decode permissions: %w", err)
	}
	set.configured = true
	for _, b := range old.Bindings {
		set.bound[Action(b.Action)] = b.Roles
	}
	for _, a := range old.Assignable {
		set.assignable[a.Role] = a.Roles
	}
	return set, nil
}

// scopes returns the scopes of every action bound to role, in a stable order.
func (p legacyPermissionSet) scopes(role string) []string {
	out := []string{}
	for _, action := range AllActions {
		if !slices.Contains(p.bound[action], role) {
			continue
		}
		for _, scope := range ActionScopes(action) {
			if !slices.Contains(out, scope) {
				out = append(out, scope)
			}
		}
	}
	return out
}

func (p legacyPermissionSet) canAssign(role string) []string {
	return append([]string{}, p.assignable[role]...)
}
