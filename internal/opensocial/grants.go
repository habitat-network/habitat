package opensocial

import (
	"slices"

	"github.com/habitat-network/habitat/api/habitat"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
)

// ReadScope is the scope granting a role read access to the space whose
// access record it appears in.
const ReadScope = "read"

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

// scopesOfRoles returns the union of scopes grants give any of roles.
func scopesOfRoles(
	grants []opensocial_api.GroupOpensocialDefsGroupGrant,
	roles []string,
) []string {
	var scopes []string
	for _, grant := range grants {
		if slices.Contains(roles, grant.Role) {
			scopes = append(scopes, grant.Scopes...)
		}
	}
	return scopes
}

// assignableRoles returns the roles holders of any of roles may assign.
func assignableRoles(
	grants []opensocial_api.GroupOpensocialDefsGroupGrant,
	roles []string,
) []string {
	var out []string
	for _, grant := range grants {
		if slices.Contains(roles, grant.Role) {
			out = append(out, grant.Assignable...)
		}
	}
	return out
}

// ActionBindings reads which roles hold each action, and which roles each
// role may assign, back out of a permissions record's grants: the
// per-role view the Habitat UI edits. A role is bound to an action when its
// own scopes cover the action's scopes.
func ActionBindings(
	grants []opensocial_api.GroupOpensocialDefsGroupGrant,
) ([]habitat.NetworkHabitatOpensocialDefsActionBinding, []habitat.NetworkHabitatOpensocialDefsAssignableBinding) {
	var bindings []habitat.NetworkHabitatOpensocialDefsActionBinding
	for _, action := range AllActions {
		var roles []string
		for _, grant := range grants {
			if NewScopeSet(grant.Scopes).CanPerform(action) {
				roles = append(roles, grant.Role)
			}
		}
		if len(roles) > 0 {
			bindings = append(bindings, habitat.NetworkHabitatOpensocialDefsActionBinding{
				Action: string(action), Roles: roles,
			})
		}
	}
	var assignable []habitat.NetworkHabitatOpensocialDefsAssignableBinding
	for _, grant := range grants {
		if len(grant.Assignable) > 0 {
			assignable = append(assignable, habitat.NetworkHabitatOpensocialDefsAssignableBinding{
				Role: grant.Role, Roles: grant.Assignable,
			})
		}
	}
	return bindings, assignable
}

// permissionGrants turns action bindings into per-role group grants: each
// role gets the scopes of every action bound to it.
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
			for _, scope := range ActionScopes(Action(binding.Action)) {
				if !slices.Contains(g.Scopes, scope) {
					g.Scopes = append(g.Scopes, scope)
				}
			}
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

// withReadRoles returns grants with exactly roles holding the read scope:
// other scopes a role holds are kept, and roles left with none are dropped.
func withReadRoles(
	grants []opensocial_api.GroupOpensocialDefsSpaceGrant,
	roles []string,
) []opensocial_api.GroupOpensocialDefsSpaceGrant {
	var out []opensocial_api.GroupOpensocialDefsSpaceGrant
	for _, grant := range grants {
		scopes := slices.DeleteFunc(slices.Clone(grant.Scopes), func(scope string) bool {
			return scope == ReadScope
		})
		if slices.Contains(roles, grant.Role) {
			scopes = append([]string{ReadScope}, scopes...)
		}
		if len(scopes) > 0 {
			out = append(
				out,
				opensocial_api.GroupOpensocialDefsSpaceGrant{Role: grant.Role, Scopes: scopes},
			)
		}
	}
	for _, role := range roles {
		if !slices.ContainsFunc(out, func(g opensocial_api.GroupOpensocialDefsSpaceGrant) bool {
			return g.Role == role
		}) {
			out = append(out, opensocial_api.GroupOpensocialDefsSpaceGrant{
				Role: role, Scopes: []string{ReadScope},
			})
		}
	}
	return out
}
