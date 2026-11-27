package opensocial

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// ErrEscalation is returned when a write to a role or a space policy would
// grant a permission, role, or access the writer does not hold, or change a
// role the writer may not assign.
var ErrEscalation = errors.New(
	"write grants permissions, roles or access beyond what the caller holds",
)

func addedTo(old, updated []string) []string {
	var added []string
	for _, x := range updated {
		if !slices.Contains(old, x) {
			added = append(added, x)
		}
	}
	return added
}

func sameSet(a, b []string) bool {
	return len(addedTo(a, b)) == 0 && len(addedTo(b, a)) == 0
}

// checkGrants enforces the spec's escalation rule for one role's entry: it may
// be written only if the role is one the writer may assign, every permission
// it adds is covered by the writer's own, and every canAssign entry it adds is
// one the writer may assign (and `*` only by a writer who holds `*`).
func checkGrants(
	scopes ScopeSet,
	assign AssignSet,
	role string,
	oldPermissions, newPermissions, oldAssign, newAssign []string,
) error {
	if !assign.Has(role) {
		return fmt.Errorf("%w: role %q is not assignable by the caller", ErrEscalation, role)
	}
	for _, permission := range addedTo(oldPermissions, newPermissions) {
		if !scopes.Covers(permission) {
			return fmt.Errorf("%w: permission %q", ErrEscalation, permission)
		}
	}
	for _, entry := range addedTo(oldAssign, newAssign) {
		if entry == AssignAll && !assign.HasAll() || !assign.Has(entry) {
			return fmt.Errorf("%w: canAssign %q", ErrEscalation, entry)
		}
	}
	return nil
}

// UpdateRole writes role on behalf of actor, refusing writes that escalate
// beyond actor's own permissions and assignable roles. The group DID itself
// may write anything.
func (s *Store) UpdateRole(
	ctx context.Context,
	orgDID, actor syntax.DID,
	rkey syntax.RecordKey,
	role Role,
) error {
	if actor != orgDID {
		roles, err := s.GetRoles(ctx, orgDID)
		if err != nil {
			return err
		}
		current := roles[string(rkey)]
		scopes, err := s.UserScopes(ctx, orgDID, actor)
		if err != nil {
			return err
		}
		assign, err := s.AssignableRoles(ctx, orgDID, actor)
		if err != nil {
			return err
		}
		if err := checkGrants(
			scopes, assign, string(rkey),
			current.Permissions, role.Permissions, current.CanAssign, role.CanAssign,
		); err != nil {
			return err
		}
	}
	return s.PutRole(ctx, orgDID, rkey, role)
}

// RemoveRole deletes a role on behalf of actor, who must be able to assign it.
func (s *Store) RemoveRole(
	ctx context.Context,
	orgDID, actor syntax.DID,
	rkey syntax.RecordKey,
) error {
	if actor != orgDID {
		assign, err := s.AssignableRoles(ctx, orgDID, actor)
		if err != nil {
			return err
		}
		if !assign.Has(string(rkey)) {
			return fmt.Errorf(
				"%w: role %q is not assignable by the caller", ErrEscalation, rkey,
			)
		}
	}
	return s.DeleteRole(ctx, orgDID, rkey)
}

// UpdatePolicy replaces space's policy on behalf of actor, with the same
// escalation rule as UpdateRole: the actor may change grants only for roles
// they may assign, any permission they add must be one they hold, and any
// read or write access they grant must already be theirs on the space.
func (s *Store) UpdatePolicy(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	actor syntax.DID,
	updated spaces.Policy,
) error {
	orgDID := space.SpaceOwner()
	current, err := s.GetPolicy(ctx, space)
	if err != nil {
		return fmt.Errorf("get space: %w", err)
	}
	if actor != orgDID {
		if err := s.checkPolicyChange(ctx, space, actor, current, updated); err != nil {
			return err
		}
	}
	return s.SetPolicy(ctx, space, updated)
}

func grantOf(policy spaces.Policy, role string) (spaces.GroupGrant, bool) {
	for _, grant := range policy.Grants {
		if grant.Role == role {
			return grant, true
		}
	}
	return spaces.GroupGrant{}, false
}

func (s *Store) checkPolicyChange(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	actor syntax.DID,
	current, updated spaces.Policy,
) error {
	orgDID := space.SpaceOwner()
	scopes, err := s.SpaceScopes(ctx, actor, space)
	if err != nil {
		return err
	}
	assign, err := s.AssignableRoles(ctx, orgDID, actor)
	if err != nil {
		return err
	}
	actorRoles, err := s.GetUserRoles(ctx, orgDID, actor)
	if err != nil {
		return fmt.Errorf("get user roles: %w", err)
	}
	canRead := HoldsRead(current, actorRoles)
	canWrite := HoldsWrite(current, actorRoles)
	if updated.PublicRead && !current.PublicRead && !canRead {
		return fmt.Errorf("%w: public read on a space the caller cannot read", ErrEscalation)
	}
	var roles []string
	for _, grant := range slices.Concat(current.Grants, updated.Grants) {
		if !slices.Contains(roles, grant.Role) {
			roles = append(roles, grant.Role)
		}
	}
	for _, role := range roles {
		before, _ := grantOf(current, role)
		after, _ := grantOf(updated, role)
		if before.Read == after.Read && before.Write == after.Write &&
			sameSet(before.Permissions, after.Permissions) {
			continue
		}
		if err := checkGrants(
			scopes, assign, role, before.Permissions, after.Permissions, nil, nil,
		); err != nil {
			return err
		}
		if after.Read && !before.Read && !canRead {
			return fmt.Errorf("%w: read access to a space the caller cannot read", ErrEscalation)
		}
		if after.Write && !before.Write && !canWrite {
			return fmt.Errorf("%w: write access to a space the caller cannot write", ErrEscalation)
		}
	}
	return nil
}

// UpdateSpaceAccess replaces the roles that may read and write a space on
// behalf of actor, leaving the permissions its policy grants roles for acting
// as the group untouched. It is UpdatePolicy with the rest of the policy kept.
func (s *Store) UpdateSpaceAccess(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	actor syntax.DID,
	roles []string,
) error {
	current, err := s.GetPolicy(ctx, space)
	if err != nil {
		return fmt.Errorf("get space: %w", err)
	}
	updated := current
	updated.Grants = nil
	for _, grant := range current.Grants {
		grant.Read, grant.Write = slices.Contains(roles, grant.Role), slices.Contains(roles, grant.Role)
		if grant.Read || len(grant.Permissions) > 0 {
			updated.Grants = append(updated.Grants, grant)
		}
	}
	for _, role := range roles {
		if _, ok := grantOf(current, role); !ok {
			updated.Grants = append(updated.Grants, spaces.GroupGrant{
				Role: role, Read: true, Write: true,
			})
		}
	}
	return s.UpdatePolicy(ctx, space, actor, updated)
}

// UpdatePermissions rebinds Habitat's actions on behalf of actor: each role
// ends up holding exactly the actions bindings name it in, and assignable
// gives each role's canAssign list. Scopes on a role that are not action
// bindings are kept. Each changed role goes through the escalation rule of
// UpdateRole, so an actor can bind only actions they hold themselves.
func (s *Store) UpdatePermissions(
	ctx context.Context,
	orgDID, actor syntax.DID,
	bindings []habitat.NetworkHabitatOpensocialDefsActionBinding,
	assignable []habitat.NetworkHabitatOpensocialDefsAssignableBinding,
) error {
	roles, err := s.GetRoles(ctx, orgDID)
	if err != nil {
		return err
	}
	// Rewrite the actor's own roles last: narrowing them first would shrink
	// what the actor may change in the rest.
	held, err := s.GetUserRoles(ctx, orgDID, actor)
	if err != nil {
		return err
	}
	ids := slices.Sorted(maps.Keys(roles))
	slices.SortStableFunc(ids, func(a, b string) int {
		return cmp.Compare(boolInt(slices.Contains(held, a)), boolInt(slices.Contains(held, b)))
	})
	for _, id := range ids {
		role := roles[id]
		var permissions []string
		for _, permission := range role.Permissions {
			if !strings.HasPrefix(permission, HabitatActionScopePrefix) {
				permissions = append(permissions, permission)
			}
		}
		for _, binding := range bindings {
			if slices.Contains(binding.Roles, id) {
				permissions = append(permissions, ActionScope(Action(binding.Action)))
			}
		}
		var canAssign []string
		for _, binding := range assignable {
			if binding.Role == id {
				canAssign = append(canAssign, binding.Roles...)
			}
		}
		if sameSet(role.Permissions, permissions) && sameSet(role.CanAssign, canAssign) {
			continue
		}
		role.Permissions, role.CanAssign = permissions, canAssign
		if err := s.UpdateRole(ctx, orgDID, actor, syntax.RecordKey(id), role); err != nil {
			return err
		}
	}
	return nil
}

// SetRoleProfile creates a role or renames an existing one on behalf of actor,
// keeping an existing role's permissions, canAssign list and default flag.
func (s *Store) SetRoleProfile(
	ctx context.Context,
	orgDID, actor syntax.DID,
	rkey syntax.RecordKey,
	name, description string,
) error {
	roles, err := s.GetRoles(ctx, orgDID)
	if err != nil {
		return err
	}
	role := roles[string(rkey)]
	role.DisplayName, role.Description = name, description
	return s.UpdateRole(ctx, orgDID, actor, rkey, role)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
