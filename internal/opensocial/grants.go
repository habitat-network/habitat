package opensocial

import (
	"slices"
	"strings"

	"github.com/habitat-network/habitat/api/habitat"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
)

// This file is the interim bridge between the spec's scope-based records
// (group.opensocial.access and group.opensocial.permissions, whose grants
// give each role OAuth scopes) and the action-binding model internal/opensocial
// still enforces. Scope-based authorization replaces it, at which point this
// file goes away.
//
// Until then a role's actions are stored as scopes in the HabitatActionScope
// namespace, and the ability to read a space is the spec's `read` scope.

const (
	// ReadScope is the spec's scope granting a role read access to a space.
	ReadScope = "read"
	// HabitatActionScopePrefix namespaces scopes that stand for one of
	// Habitat's actions, e.g. "habitat:action=invite".
	HabitatActionScopePrefix = "habitat:action="
)

// ActionScope returns the scope that stands for action.
func ActionScope(action Action) string {
	return HabitatActionScopePrefix + string(action)
}

// readGrants returns space grants giving each of roles the read scope.
func readGrants(roles []string) []opensocial_api.GroupOpensocialDefsSpaceGrant {
	grants := make([]opensocial_api.GroupOpensocialDefsSpaceGrant, len(roles))
	for i, role := range roles {
		grants[i] = opensocial_api.GroupOpensocialDefsSpaceGrant{
			Role:   role,
			Scopes: []string{ReadScope},
		}
	}
	return grants
}

// ReadRoles returns the roles in grants that hold the read scope.
func ReadRoles(grants []opensocial_api.GroupOpensocialDefsSpaceGrant) []string {
	var roles []string
	for _, grant := range grants {
		if slices.Contains(grant.Scopes, ReadScope) {
			roles = append(roles, grant.Role)
		}
	}
	return roles
}

// actionRoles returns, for each action scope in grants, the roles holding it.
func actionRoles(
	grants []opensocial_api.GroupOpensocialDefsGroupGrant,
) map[Action][]string {
	byAction := map[Action][]string{}
	for _, grant := range grants {
		for _, scope := range grant.Scopes {
			if action, ok := strings.CutPrefix(scope, HabitatActionScopePrefix); ok {
				byAction[Action(action)] = append(byAction[Action(action)], grant.Role)
			}
		}
	}
	return byAction
}

// assignableRoles returns the roles holders of role may assign in grants.
func assignableRoles(
	grants []opensocial_api.GroupOpensocialDefsGroupGrant,
	role string,
) []string {
	var out []string
	for _, grant := range grants {
		if grant.Role == role {
			out = append(out, grant.Assignable...)
		}
	}
	return out
}

// permissionGrants inverts action bindings into per-role group grants.
func permissionGrants(
	bindings []habitat.NetworkHabitatOpensocialDefsActionBinding,
	assignable []habitat.NetworkHabitatOpensocialDefsAssignableBinding,
) []opensocial_api.GroupOpensocialDefsGroupGrant {
	byRole := map[string]*opensocial_api.GroupOpensocialDefsGroupGrant{}
	var order []string
	grantFor := func(role string) *opensocial_api.GroupOpensocialDefsGroupGrant {
		if g, ok := byRole[role]; ok {
			return g
		}
		g := &opensocial_api.GroupOpensocialDefsGroupGrant{
			Role:       role,
			Scopes:     []string{},
			Assignable: []string{},
		}
		byRole[role] = g
		order = append(order, role)
		return g
	}
	for _, binding := range bindings {
		for _, role := range binding.Roles {
			g := grantFor(role)
			g.Scopes = append(g.Scopes, ActionScope(Action(binding.Action)))
		}
	}
	for _, binding := range assignable {
		g := grantFor(binding.Role)
		g.Assignable = append(g.Assignable, binding.Roles...)
	}
	grants := make([]opensocial_api.GroupOpensocialDefsGroupGrant, len(order))
	for i, role := range order {
		grants[i] = *byRole[role]
	}
	return grants
}
