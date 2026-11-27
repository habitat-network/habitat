package opensocial

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/bluesky-social/indigo/atproto/syntax"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// ErrHostMaintained is returned for a direct write to a record only the host
// writes: the group's space index and the published space policies.
var ErrHostMaintained = errors.New("this record is maintained by the host")

// keepsAdmin returns ErrLastAdmin when giving member newRoles would leave the
// group with no admin.
func (s *Store) keepsAdmin(
	ctx context.Context,
	orgDID, member syntax.DID,
	newRoles []string,
) error {
	if slices.Contains(newRoles, AdminRoleRkey) {
		return nil
	}
	current, err := s.GetUserRoles(ctx, orgDID, member)
	if err != nil {
		return err
	}
	if !slices.Contains(current, AdminRoleRkey) {
		return nil
	}
	collection := syntax.NSID(MembershipCollection)
	records, err := s.spacesStore.ListRecords(ctx, membersSpaceOf(orgDID), orgDID, &collection)
	if err != nil {
		return fmt.Errorf("list membership records: %w", err)
	}
	for _, record := range records {
		if record.Rkey == syntax.RecordKey(member) {
			continue
		}
		var membership opensocial_api.OrgAtgrpMembership
		if err := decodeRecordValue(record.Value, &membership); err != nil {
			return fmt.Errorf("decode membership record: %w", err)
		}
		if slices.Contains(membership.Roles, AdminRoleRkey) {
			return nil
		}
	}
	return ErrLastAdmin
}

// GroupWrite is a direct write to a record in one of a group's spaces by a
// session acting as the group.
type GroupWrite struct {
	Group      syntax.DID
	Actor      syntax.DID // the member the session acts as; the group itself for a legacy session
	Scopes     ScopeSet
	Space      habitat_syntax.SpaceURI
	Collection syntax.NSID
	Rkey       syntax.RecordKey
	Value      map[string]any // nil for a delete
}

// AuthorizeGroupWrite applies the spec's rules to a record write by a session
// acting as the group. Records only the host writes are refused outright. In the
// meta and members spaces the session must hold the space scope for the
// collection and action, role changes must stay within the actor's `canAssign`
// roles and held permissions, and the group may not lose its last admin.
func (s *Store) AuthorizeGroupWrite(ctx context.Context, write GroupWrite) error {
	switch write.Collection.String() {
	case SpaceIndexCollection, SpacePolicyCollection:
		return ErrHostMaintained
	}
	spaceType := write.Space.SpaceType().String()
	if spaceType != MetaSpaceType && spaceType != MembersSpaceType {
		return nil
	}
	action := "delete"
	if write.Value != nil {
		action = "create"
		if _, err := s.spacesStore.GetRecord(
			ctx, write.Space, write.Group, write.Collection, write.Rkey,
		); err == nil {
			action = "update"
		} else if !errors.Is(err, spaces.ErrRecordNotFound) {
			return fmt.Errorf("get record: %w", err)
		}
	}
	required := fmt.Sprintf(
		"space:%s?authority=self&skey=%s&collection=%s&action=%s",
		spaceType, write.Space.Skey(), write.Collection, action,
	)
	if !write.Scopes.Covers(required) {
		return fmt.Errorf("%w: missing %s", ErrEscalation, required)
	}
	// A record written without a key gets a generated one, which no
	// membership or role is read from.
	if spaceType != MembersSpaceType || write.Rkey == "" {
		return nil
	}
	switch write.Collection.String() {
	case MembershipCollection:
		return s.authorizeMembershipWrite(ctx, write)
	case RoleCollection:
		return s.authorizeRoleWrite(ctx, write)
	}
	return nil
}

func (s *Store) authorizeMembershipWrite(ctx context.Context, write GroupWrite) error {
	member := syntax.DID(write.Rkey)
	var target []string
	if write.Value != nil {
		var membership opensocial_api.OrgAtgrpMembership
		if err := decodeRecordValue(write.Value, &membership); err != nil {
			return fmt.Errorf("decode membership record: %w", err)
		}
		target = membership.Roles
	}
	current, err := s.GetUserRoles(ctx, write.Group, member)
	if err != nil {
		return err
	}
	if write.Actor != write.Group {
		ok, err := s.CanAssignRoles(ctx, write.Group, write.Actor, current, target)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: a role of this membership is not assignable", ErrEscalation)
		}
	}
	return s.keepsAdmin(ctx, write.Group, member, target)
}

func (s *Store) authorizeRoleWrite(ctx context.Context, write GroupWrite) error {
	if write.Value == nil {
		if write.Actor != write.Group {
			assign, err := s.AssignableRoles(ctx, write.Group, write.Actor)
			if err != nil {
				return err
			}
			if !assign.Has(string(write.Rkey)) {
				return fmt.Errorf("%w: role %q is not assignable", ErrEscalation, write.Rkey)
			}
		}
		if write.Rkey == AdminRoleRkey || write.Rkey == MemberRoleRkey {
			return ErrCannotDeleteBuiltinRole
		}
		return nil
	}
	if !ValidRoleID(string(write.Rkey)) {
		return ErrInvalidRoleID
	}
	if write.Actor == write.Group {
		return nil
	}
	var updated Role
	if err := decodeRecordValue(write.Value, &updated); err != nil {
		return fmt.Errorf("decode role record: %w", err)
	}
	roles, err := s.GetRoles(ctx, write.Group)
	if err != nil {
		return err
	}
	current := roles[string(write.Rkey)]
	assign, err := s.AssignableRoles(ctx, write.Group, write.Actor)
	if err != nil {
		return err
	}
	scopes, err := s.UserScopes(ctx, write.Group, write.Actor)
	if err != nil {
		return err
	}
	return checkGrants(
		scopes, assign, string(write.Rkey),
		current.Permissions, updated.Permissions, current.CanAssign, updated.CanAssign,
	)
}
