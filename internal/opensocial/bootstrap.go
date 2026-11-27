package opensocial

import (
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/spaces"
)

// bootstrapRoles returns the roles a new group starts with: admins hold every
// permission and may assign any role; members, the default role, hold nothing
// beyond what the meta and members policies give them.
func bootstrapRoles() map[string]opensocial_api.OrgAtgrpRole {
	return map[string]opensocial_api.OrgAtgrpRole{
		AdminRoleRkey: {
			DisplayName: "Admin",
			Permissions: []string{
				"include:org.atgrp.authManageAdmin",
				"include:org.atgrp.authManageBasic",
				"include:org.atgrp.authManageMembers",
				"include:org.atgrp.authModerate",
				"include:org.atgrp.authParticipate",
				"space:*?manage=create&manage=update&manage=delete",
				"space:*?collection=" + LabelCollection,
				McpConfigureScope,
			},
			CanAssign: []string{AssignAll},
		},
		MemberRoleRkey: {
			DisplayName: "Member",
			// Members admitted through requestJoin get this role.
			Default: true,
		},
	}
}

// bootstrapPolicy is the policy of a new group's meta or members space.
// Members read both spaces and write to members, where they keep their own
// confirmation and profile; admins can do both and write in meta too.
func bootstrapPolicy(public, members bool) spaces.Policy {
	member := spaces.GroupGrant{Role: MemberRoleRkey, Read: true, Write: members}
	admin := spaces.GroupGrant{Role: AdminRoleRkey, Read: true, Write: true}
	return spaces.Policy{
		Type:       spaces.PolicyGroupSpace,
		PublicRead: public,
		Grants:     []spaces.GroupGrant{member, admin},
	}
}
