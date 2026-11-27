package opensocial

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// ErrEscalation is returned when a write to an access or permissions record
// would grant a scope or role the writer does not hold, or change a role the
// writer may not assign.
var ErrEscalation = errors.New(
	"write grants scopes or roles beyond what the caller holds",
)

// grantChange is one role's entry in an access or permissions record, before
// and after a write.
type grantChange struct {
	role       string
	oldScopes  []string
	newScopes  []string
	oldAssign  []string
	newAssign  []string
	oldPresent bool
	newPresent bool
}

func sameSet(a, b []string) bool {
	return len(a) == len(b) && !slices.ContainsFunc(a, func(x string) bool {
		return !slices.Contains(b, x)
	})
}

func addedTo(old, updated []string) []string {
	var added []string
	for _, x := range updated {
		if !slices.Contains(old, x) {
			added = append(added, x)
		}
	}
	return added
}

func groupChanges(
	old, updated []opensocial_api.GroupOpensocialDefsGroupGrant,
) []grantChange {
	byRole := map[string]*grantChange{}
	var order []string
	get := func(role string) *grantChange {
		if c, ok := byRole[role]; ok {
			return c
		}
		c := &grantChange{role: role}
		byRole[role] = c
		order = append(order, role)
		return c
	}
	for _, g := range old {
		c := get(g.Role)
		c.oldPresent, c.oldScopes, c.oldAssign = true, g.Scopes, g.Assignable
	}
	for _, g := range updated {
		c := get(g.Role)
		c.newPresent, c.newScopes, c.newAssign = true, g.Scopes, g.Assignable
	}
	var out []grantChange
	for _, role := range order {
		c := byRole[role]
		if c.oldPresent != c.newPresent ||
			!sameSet(c.oldScopes, c.newScopes) || !sameSet(c.oldAssign, c.newAssign) {
			out = append(out, *c)
		}
	}
	return out
}

func spaceChanges(
	old, updated []opensocial_api.GroupOpensocialDefsSpaceGrant,
) []grantChange {
	var oldG, newG []opensocial_api.GroupOpensocialDefsGroupGrant
	for _, g := range old {
		oldG = append(oldG, opensocial_api.GroupOpensocialDefsGroupGrant{Role: g.Role, Scopes: g.Scopes})
	}
	for _, g := range updated {
		newG = append(newG, opensocial_api.GroupOpensocialDefsGroupGrant{Role: g.Role, Scopes: g.Scopes})
	}
	return groupChanges(oldG, newG)
}

// checkChanges enforces the spec's escalation rule: the writer may change
// entries only for roles they may assign, and may add only scopes they hold
// and assignable roles they can assign.
func checkChanges(
	scopes ScopeSet,
	assignable []string,
	changed []grantChange,
) error {
	for _, c := range changed {
		if !slices.Contains(assignable, c.role) {
			return fmt.Errorf("%w: role %q is not assignable by the caller", ErrEscalation, c.role)
		}
		for _, scope := range addedTo(c.oldScopes, c.newScopes) {
			if !scopes.Covers(scope) {
				return fmt.Errorf("%w: scope %q", ErrEscalation, scope)
			}
		}
		for _, role := range addedTo(c.oldAssign, c.newAssign) {
			if !slices.Contains(assignable, role) {
				return fmt.Errorf("%w: assignable role %q", ErrEscalation, role)
			}
		}
	}
	return nil
}

// UpdatePermissions replaces orgDID's permissions record on behalf of actor,
// refusing writes that escalate beyond actor's own scopes and assignable
// roles. The group DID itself may write anything.
func (s *Store) UpdatePermissions(
	ctx context.Context,
	orgDID, actor syntax.DID,
	bindings []habitat.NetworkHabitatOpensocialDefsActionBinding,
	assignable []habitat.NetworkHabitatOpensocialDefsAssignableBinding,
) error {
	if actor != orgDID {
		current, _, err := s.getPermissions(ctx, orgDID)
		if err != nil {
			return err
		}
		scopes, err := s.UserScopes(ctx, orgDID, actor)
		if err != nil {
			return err
		}
		assignableRoles, err := s.AssignableRoles(ctx, orgDID, actor)
		if err != nil {
			return err
		}
		if err := checkChanges(
			scopes, assignableRoles,
			groupChanges(current.Grants, permissionGrants(bindings, assignable)),
		); err != nil {
			return err
		}
	}
	return s.PutPermissions(ctx, orgDID, bindings, assignable)
}

// UpdateSpaceAccess replaces the roles that may read a space on behalf of
// actor, with the same escalation rule as UpdatePermissions. A role's read
// scope counts as held when actor's own grants in the space include it.
func (s *Store) UpdateSpaceAccess(
	ctx context.Context,
	spaceURI habitat_syntax.SpaceURI,
	actor syntax.DID,
	roles []string,
) error {
	orgDID := spaceURI.SpaceOwner()
	// Report a missing space before judging the grants written into it.
	if _, err := s.spacesStore.GetSpacePolicy(ctx, spaceURI); err != nil {
		return fmt.Errorf("get space: %w", err)
	}
	if actor != orgDID {
		current, err := s.getAccess(ctx, spaceURI)
		if err != nil {
			return err
		}
		actorRoles, err := s.GetUserRoles(ctx, orgDID, actor)
		if err != nil {
			return fmt.Errorf("get user roles: %w", err)
		}
		var own []string
		for _, grant := range current.Grants {
			if slices.Contains(actorRoles, grant.Role) {
				own = append(own, grant.Scopes...)
			}
		}
		groupScopes, err := s.UserScopes(ctx, orgDID, actor)
		if err != nil {
			return err
		}
		assignableRoles, err := s.AssignableRoles(ctx, orgDID, actor)
		if err != nil {
			return err
		}
		held := ScopeSet{all: groupScopes.all, scopes: append(own, groupScopes.scopes...)}
		if err := checkChanges(
			held, assignableRoles,
			spaceChanges(current.Grants, withReadRoles(current.Grants, roles)),
		); err != nil {
			return err
		}
	}
	return s.UpdateSpace(ctx, spaceURI, roles)
}
