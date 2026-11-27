package opensocial

import opensocial_api "github.com/habitat-network/habitat/api/opensocial"

// bootstrapGrants returns the permissions a new group starts with: admins hold
// every scope and may assign both built-in roles; members hold the participate
// permission set.
func bootstrapGrants() []opensocial_api.OrgAtgrpDefsGroupGrant {
	return []opensocial_api.OrgAtgrpDefsGroupGrant{
		{
			Role: AdminRoleRkey,
			Scopes: []string{
				"include:org.atgrp.configure",
				"include:org.atgrp.manageSpaces",
				"include:org.atgrp.manageMembers",
				"include:org.atgrp.moderate",
				"include:org.atgrp.participate",
				"space:*?manage=create&manage=delete",
				McpConfigureScope,
			},
			Assignable: []string{AdminRoleRkey, MemberRoleRkey},
		},
		{
			Role:       MemberRoleRkey,
			Scopes:     []string{"include:org.atgrp.participate"},
			Assignable: []string{},
		},
	}
}
