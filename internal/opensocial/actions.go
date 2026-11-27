package opensocial

// Action is one of the Habitat actions a role can be authorized to perform.
// The spec grants roles scopes rather than named actions, so each action is
// shorthand for the scopes it requires: a member may perform an action when
// the union of their roles' scopes covers all of ActionScopes(action).
type Action string

const (
	ActionInvite             Action = "invite"
	ActionEject              Action = "eject"
	ActionRoleAssign         Action = "role.assign"
	ActionSpaceCreate        Action = "space.create"
	ActionSpaceConfigure     Action = "space.configure"
	ActionSpaceDelete        Action = "space.delete"
	ActionCommunityConfigure Action = "community.configure"
	ActionMcpConfigure       Action = "mcp.configure"
)

// AllActions lists every action.
var AllActions = []Action{
	ActionInvite, ActionEject, ActionRoleAssign, ActionSpaceCreate,
	ActionSpaceConfigure, ActionSpaceDelete, ActionCommunityConfigure,
	ActionMcpConfigure,
}

// HabitatActionScopePrefix namespaces scopes that stand for one of Habitat's
// actions, e.g. "habitat:action=invite": a role bound to the action holds
// every scope the action requires.
const HabitatActionScopePrefix = "habitat:action="

// ActionScope returns the scope that stands for action.
func ActionScope(action Action) string {
	return HabitatActionScopePrefix + string(action)
}

// McpConfigureScope is the Habitat-only scope for configuring MCP servers; the
// spec has no equivalent.
const McpConfigureScope = "habitat:mcp=configure"

// ActionScopes returns the scopes that an action requires.
func ActionScopes(action Action) []string {
	switch action {
	case ActionInvite:
		return []string{"space:" + InboxSpaceType +
			"?authority=*&skey=self&collection=" + InviteCollection + "&action=create"}
	case ActionEject:
		return []string{"space:" + MembersSpaceType +
			"?authority=self&skey=self&collection=" + MembershipCollection + "&action=delete"}
	case ActionRoleAssign:
		return []string{"space:" + MembersSpaceType +
			"?authority=self&skey=self&collection=" + MembershipCollection +
			"&action=create&action=update"}
	case ActionSpaceCreate:
		return []string{"space:*?manage=create"}
	case ActionSpaceDelete:
		return []string{"space:*?manage=delete"}
	case ActionSpaceConfigure:
		return []string{"space:*?manage=update"}
	case ActionCommunityConfigure:
		return []string{"include:org.atgrp.authManageAdmin"}
	case ActionMcpConfigure:
		return []string{McpConfigureScope}
	}
	return nil
}

// CanPerform reports whether scopes covers everything action requires.
func (s ScopeSet) CanPerform(action Action) bool {
	required := ActionScopes(action)
	if len(required) == 0 {
		return false
	}
	for _, scope := range required {
		if !s.Covers(scope) {
			return false
		}
	}
	return true
}
