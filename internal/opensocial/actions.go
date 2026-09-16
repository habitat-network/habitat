package opensocial

// Action is one of the standardized actions a community.opensocial.role can
// be bound to, via a community's community.opensocial.permissions record. A
// member's authorized actions are the union of the actions bound to every
// role they hold — there are no deny-rules or precedence between roles.
type Action string

const (
	ActionInvite             Action = "invite"
	ActionEject              Action = "eject"
	ActionRoleAssign         Action = "role.assign"
	ActionSpaceCreate        Action = "space.create"
	ActionSpaceConfigure     Action = "space.configure"
	ActionSpaceDelete        Action = "space.delete"
	ActionCommunityConfigure Action = "community.configure"
)
