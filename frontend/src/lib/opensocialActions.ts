// Mirrors the `action` knownValues in lexicons/community/opensocial/permissions.json
// and the internal/opensocial.Action constants (Go). Kept in sync manually
// since actions are a small, stable enumeration for the standard.
export const OPENSOCIAL_ACTIONS: {
  action: string;
  label: string;
  hint: string;
}[] = [
  {
    action: "community.configure",
    label: "Configure community",
    hint: "Edit the profile, rules, roles, and permissions.",
  },
  {
    action: "space.create",
    label: "Create spaces",
    hint: "Create a modality space under the community DID.",
  },
  {
    action: "space.configure",
    label: "Configure spaces",
    hint: "Change a space's config, including who can read it.",
  },
  { action: "space.delete", label: "Delete spaces", hint: "Remove a space." },
  {
    action: "role.assign",
    label: "Assign roles",
    hint: "Grant and revoke roles, bounded by the assignable roles below.",
  },
  {
    action: "eject",
    label: "Eject members",
    hint: "Remove a member, bounded by the assignable roles below.",
  },
  {
    action: "invite",
    label: "Invite",
    hint: "Issue invites to join the community.",
  },
  {
    action: "mcp.configure",
    label: "Configure MCP servers",
    hint: "Add, update, and remove MCP servers configured for the community.",
  },
];

export const ACTION_MCP_CONFIGURE = "mcp.configure";
export const ACTION_ROLE_ASSIGN = "role.assign";
export const ACTION_COMMUNITY_CONFIGURE = "community.configure";

// hasOpensocialAction reports whether a member holding userRoles may perform
// action under bindings (the community.opensocial.permissions record's
// action bindings). A member is authorized when any role they hold is bound
// to the action. Mirrors Store.CheckAction (Go): a community with no
// permissions record at all (surfaced here as empty bindings) falls back to
// authorizing its admins for every action.
export function hasOpensocialAction(
  bindings: { action: string; roles: string[] }[],
  userRoles: string[],
  action: string,
): boolean {
  if (bindings.length === 0) {
    return userRoles.includes("admin");
  }
  const binding = bindings.find((b) => b.action === action);
  return (binding?.roles ?? []).some((role) => userRoles.includes(role));
}

// assignableRoles returns the union, across every role userRoles holds, of
// the roles they may grant/revoke via role.assign (the permissions record's
// assignable bindings). Mirrors Store.AssignableRoles (Go): a community with
// no permissions record at all (surfaced here as empty bindings and empty
// assignable) falls back to letting its admins assign any declared role.
export function assignableRoles(
  bindings: { action: string; roles: string[] }[],
  assignable: { role: string; roles: string[] }[],
  userRoles: string[],
  declaredRoles: string[],
): string[] {
  if (bindings.length === 0 && assignable.length === 0) {
    return userRoles.includes("admin") ? declaredRoles : [];
  }
  const result = new Set<string>();
  for (const binding of assignable) {
    if (!userRoles.includes(binding.role)) continue;
    for (const role of binding.roles) result.add(role);
  }
  return [...result];
}

// canAssignRoles reports whether the role UI should be shown: the member must
// hold the role.assign action and be permitted to assign at least one role.
export function canAssignRoles(
  bindings: { action: string; roles: string[] }[],
  assignable: { role: string; roles: string[] }[],
  userRoles: string[],
  declaredRoles: string[],
): boolean {
  return (
    hasOpensocialAction(bindings, userRoles, ACTION_ROLE_ASSIGN) &&
    assignableRoles(bindings, assignable, userRoles, declaredRoles).length > 0
  );
}
