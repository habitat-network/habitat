package opensocial

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xmaps"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	PermissionsCollection = "community.opensocial.permissions"
	RoleCollection        = "community.opensocial.role"
)

var (
	ErrRoleNotFound            = errors.New("role not found")
	ErrCannotDeleteBuiltinRole = errors.New("admin and member roles cannot be deleted")
	ErrRoleNotAssignable       = errors.New("caller may not assign or revoke one of the given roles")
	ErrMemberNotFound          = errors.New("member not found")
)

// GetPermissions returns orgDID's authz configuration: which roles authorize
// which actions, and which roles each role may assign/eject. Returns a zero
// value (no bindings) if the community hasn't written one, which authorizes
// nothing.
func (s *Store) GetPermissions(
	ctx context.Context,
	orgDID syntax.DID,
) (opensocial_api.CommunityOpensocialPermissions, error) {
	record, err := s.spacesStore.GetRecord(
		ctx,
		habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
		orgDID,
		PermissionsCollection,
		"self",
	)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return opensocial_api.CommunityOpensocialPermissions{}, nil
	}
	if err != nil {
		return opensocial_api.CommunityOpensocialPermissions{}, fmt.Errorf(
			"get permissions record: %w", err,
		)
	}
	var permissions opensocial_api.CommunityOpensocialPermissions
	if err := decodeRecordValue(record.Value, &permissions); err != nil {
		return opensocial_api.CommunityOpensocialPermissions{}, fmt.Errorf(
			"decode permissions record: %w", err,
		)
	}
	return permissions, nil
}

// PutPermissions replaces orgDID's authz configuration.
func (s *Store) PutPermissions(
	ctx context.Context,
	orgDID syntax.DID,
	bindings []opensocial_api.CommunityOpensocialPermissionsActionBinding,
	assignable []opensocial_api.CommunityOpensocialPermissionsAssignableBinding,
) error {
	recordBytes, err := spaces.MarshalRecord(opensocial_api.CommunityOpensocialPermissions{
		Bindings:   bindings,
		Assignable: assignable,
		UpdatedAt:  time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("marshal permissions record: %w", err)
	}
	if _, _, err := s.spacesStore.PutRecord(
		ctx,
		habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
		orgDID,
		PermissionsCollection,
		"self",
		recordBytes,
	); err != nil {
		return fmt.Errorf("put permissions record: %w", err)
	}
	return nil
}

// CheckAction reports whether user is authorized to perform action in
// orgDID, i.e. whether any role they hold is bound to it in the community's
// permissions record.
func (s *Store) CheckAction(
	ctx context.Context,
	orgDID syntax.DID,
	user syntax.DID,
	action Action,
) (bool, error) {
	userRoles, err := s.GetUserRoles(ctx, orgDID, user)
	if err != nil {
		return false, fmt.Errorf("get user roles: %w", err)
	}
	if len(userRoles) == 0 {
		return false, nil
	}
	permissions, err := s.GetPermissions(ctx, orgDID)
	if err != nil {
		return false, fmt.Errorf("get permissions: %w", err)
	}
	userRoleSet := xmaps.SetFromSlice(userRoles)
	for _, binding := range permissions.Bindings {
		if Action(binding.Action) != action {
			continue
		}
		if len(xmaps.Intersection(userRoleSet, xmaps.SetFromSlice(binding.Roles))) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// AssignableRoles returns the union, across every role user holds in
// orgDID, of the roles they may grant/revoke via role.assign or eject a
// holder of via eject.
func (s *Store) AssignableRoles(
	ctx context.Context,
	orgDID syntax.DID,
	user syntax.DID,
) ([]string, error) {
	userRoles, err := s.GetUserRoles(ctx, orgDID, user)
	if err != nil {
		return nil, fmt.Errorf("get user roles: %w", err)
	}
	if len(userRoles) == 0 {
		return nil, nil
	}
	permissions, err := s.GetPermissions(ctx, orgDID)
	if err != nil {
		return nil, fmt.Errorf("get permissions: %w", err)
	}
	userRoleSet := xmaps.SetFromSlice(userRoles)
	assignable := xmaps.Set[string]{}
	for _, binding := range permissions.Assignable {
		if _, ok := userRoleSet[binding.Role]; !ok {
			continue
		}
		for _, role := range binding.Roles {
			assignable[role] = struct{}{}
		}
	}
	return slices.Collect(maps.Keys(assignable)), nil
}

// CanAssignRoles reports whether user may move a member between their
// current roles and targetRoles: every role added or removed must be one
// user is permitted to assign, per AssignableRoles.
func (s *Store) CanAssignRoles(
	ctx context.Context,
	orgDID syntax.DID,
	user syntax.DID,
	currentRoles []string,
	targetRoles []string,
) (bool, error) {
	assignable, err := s.AssignableRoles(ctx, orgDID, user)
	if err != nil {
		return false, fmt.Errorf("assignable roles: %w", err)
	}
	assignableSet := xmaps.SetFromSlice(assignable)
	changed := xmaps.Union(
		xmaps.Difference(xmaps.SetFromSlice(currentRoles), xmaps.SetFromSlice(targetRoles)),
		xmaps.Difference(xmaps.SetFromSlice(targetRoles), xmaps.SetFromSlice(currentRoles)),
	)
	for role := range changed {
		if _, ok := assignableSet[role]; !ok {
			return false, nil
		}
	}
	return true, nil
}

// PutRole creates or updates a role declaration.
func (s *Store) PutRole(
	ctx context.Context,
	orgDID syntax.DID,
	rkey syntax.RecordKey,
	name string,
	description string,
) error {
	role := opensocial_api.CommunityOpensocialRole{
		Name:      name,
		UpdatedAt: time.Now().Format(time.RFC3339),
	}
	if description != "" {
		role.Description = description
	}
	recordBytes, err := spaces.MarshalRecord(role)
	if err != nil {
		return fmt.Errorf("marshal role record: %w", err)
	}
	if _, _, err := s.spacesStore.PutRecord(
		ctx,
		habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
		orgDID,
		RoleCollection,
		rkey,
		recordBytes,
	); err != nil {
		return fmt.Errorf("put role record: %w", err)
	}
	return nil
}

// DeleteRole removes a role declaration. The built-in admin and member
// roles, seeded by NewOrg, may not be deleted.
func (s *Store) DeleteRole(
	ctx context.Context,
	orgDID syntax.DID,
	rkey syntax.RecordKey,
) error {
	if string(rkey) == AdminRoleRkey || string(rkey) == MemberRoleRkey {
		return ErrCannotDeleteBuiltinRole
	}
	membersSpace := habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self")
	if _, err := s.spacesStore.GetRecord(
		ctx, membersSpace, orgDID, RoleCollection, rkey,
	); errors.Is(err, spaces.ErrRecordNotFound) {
		return ErrRoleNotFound
	} else if err != nil {
		return fmt.Errorf("get role record: %w", err)
	}
	if err := s.spacesStore.DeleteRecord(
		ctx, membersSpace, orgDID, RoleCollection, string(rkey),
	); err != nil {
		return fmt.Errorf("delete role record: %w", err)
	}
	return nil
}

// EjectMember removes a member from the community, revoking their roles (and
// so their access) by deleting their membership record.
func (s *Store) EjectMember(
	ctx context.Context,
	orgDID syntax.DID,
	member syntax.DID,
) error {
	membersSpace := habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self")
	if _, err := s.spacesStore.GetRecord(
		ctx, membersSpace, orgDID, MembershipCollection, syntax.RecordKey(member),
	); errors.Is(err, spaces.ErrRecordNotFound) {
		return ErrMemberNotFound
	} else if err != nil {
		return fmt.Errorf("get membership record: %w", err)
	}
	if err := s.spacesStore.DeleteRecord(
		ctx, membersSpace, orgDID, MembershipCollection, string(member),
	); err != nil {
		return fmt.Errorf("delete membership record: %w", err)
	}
	return nil
}
