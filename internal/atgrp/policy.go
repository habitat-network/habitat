package atgrp

import (
	"context"
	"fmt"
	"slices"

	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// ValidatePolicyChange prevents a role holder from delegating more authority
// than they hold before updating an existing canonical group policy. The
// caller checks the token and the actor's live manage permission, loads the
// actor and policies in WithGroupTx, preserves a recovery administrator, and
// reconciles writers when applying the policy.
func (s *Store) ValidatePolicyChange(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	actor Access,
	before, after spaces.Policy,
) error {
	group := space.SpaceOwner()
	if !actor.Member || actor.group != group || actor.Permissions.all {
		return fmt.Errorf("%w: actor is not a member of the space's group", ErrWriteForbidden)
	}
	if before.Type != spaces.PolicyAtgrp || after.Type != spaces.PolicyAtgrp {
		return fmt.Errorf("%w: group policies must remain canonical", ErrWriteForbidden)
	}
	read, write := actor.PersonalSpaceAccess(space, before)
	if after.PublicRead && !before.PublicRead && !read {
		return fmt.Errorf(
			"%w: authorizer cannot make a space public they cannot read",
			ErrWriteForbidden,
		)
	}

	old := make(map[string]spaces.AtgrpGrant, len(before.Grants))
	for _, grant := range before.Grants {
		old[grant.Role] = grant
	}
	seen := make(map[string]bool, len(after.Grants))
	held := actor.SpacePermissions(space, before)
	for _, next := range after.Grants {
		role := next.Role
		if !ValidRoleID(role) || seen[role] {
			return fmt.Errorf("%w: invalid or duplicate policy role %q", ErrInvalidRecord, role)
		}
		seen[role] = true
		prev, existed := old[role]
		delete(old, role)
		if existed && prev.Read == next.Read && prev.Write == next.Write &&
			slices.Equal(prev.Permissions, next.Permissions) {
			continue
		}
		if !actor.CanAssignRole(role) {
			return fmt.Errorf(
				"%w: authorizer cannot change grants for role %q",
				ErrWriteForbidden,
				role,
			)
		}
		if _, err := s.getRole(ctx, group, role); err != nil {
			return fmt.Errorf("get policy role %q: %w", role, err)
		}
		if next.Read && !prev.Read && !read || next.Write && !prev.Write && !write {
			return fmt.Errorf(
				"%w: policy adds space access the authorizer lacks",
				ErrWriteForbidden,
			)
		}
		if err := validatePolicyPermissions(
			space,
			actor,
			held,
			prev.Permissions,
			next.Permissions,
		); err != nil {
			return err
		}
	}
	for role := range old {
		if !actor.CanAssignRole(role) {
			return fmt.Errorf(
				"%w: authorizer cannot remove grants for role %q",
				ErrWriteForbidden,
				role,
			)
		}
	}
	return nil
}

func validatePolicyPermissions(
	space habitat_syntax.SpaceURI,
	actor Access,
	held PermissionSet,
	before, after []string,
) error {
	var old PermissionSet
	for _, perm := range ParsePermissions(actor.group, before).grants {
		if bound, ok := bindPolicyPermission(space, perm); ok {
			old.grants = append(old.grants, bound)
		}
	}
	for _, scope := range after {
		if slices.Contains(before, scope) {
			continue
		}
		grants, err := parseScope(actor.group, scope)
		if err != nil {
			return fmt.Errorf("%w: invalid policy permission %q", ErrInvalidRecord, scope)
		}
		found := false
		for _, perm := range grants {
			grant, ok := bindPolicyPermission(space, perm)
			if !ok {
				continue
			}
			found = true
			if !old.allows(grant) && !held.allows(grant) {
				return fmt.Errorf(
					"%w: policy adds a permission the authorizer lacks",
					ErrWriteForbidden,
				)
			}
		}
		if !found {
			return fmt.Errorf("%w: permission does not apply to this space", ErrInvalidRecord)
		}
	}
	return nil
}
