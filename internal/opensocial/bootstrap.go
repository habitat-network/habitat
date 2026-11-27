package opensocial

import opensocial_api "github.com/habitat-network/habitat/api/opensocial"

// bootstrapGrants returns the permissions a new group starts with: admins hold
// every scope and may assign both built-in roles; members hold the participate
// permission set.
func bootstrapGrants() []opensocial_api.GroupOpensocialDefsGroupGrant {
	return []opensocial_api.GroupOpensocialDefsGroupGrant{
		{
			Role: AdminRoleRkey,
			Scopes: []string{
				"include:group.opensocial.configure",
				"include:group.opensocial.manageSpaces",
				"include:group.opensocial.manageMembers",
				"include:group.opensocial.moderate",
				"include:group.opensocial.participate",
				"space:*?manage=create&manage=delete",
				McpConfigureScope,
			},
			Assignable: []string{AdminRoleRkey, MemberRoleRkey},
		},
		{
			Role:       MemberRoleRkey,
			Scopes:     []string{"include:group.opensocial.participate"},
			Assignable: []string{},
		},
	}
}
