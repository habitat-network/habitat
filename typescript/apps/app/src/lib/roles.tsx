import { queryOptions } from "@tanstack/react-query";
import { getRolesAndPermissions } from "@/server/fns/manage";

export const rolesQuery = (org: string) =>
  queryOptions({
    queryKey: ["roles", org],
    queryFn: () => getRolesAndPermissions({ data: { org } }),
  });

// The actions pear checks (community.opensocial.permissions), with what
// each one lets a role do, in the order the roles page lists them.
export const ACTIONS: { id: string; label: string; description: string }[] = [
  {
    id: "invite",
    label: "Invite members",
    description: "Send and revoke invites",
  },
  {
    id: "eject",
    label: "Remove members",
    description: "Remove someone from the group",
  },
  {
    id: "role.assign",
    label: "Assign roles",
    description: "Change members' roles, within what they may assign",
  },
  {
    id: "space.create",
    label: "Create spaces",
    description: "Create new spaces owned by the group",
  },
  {
    id: "space.configure",
    label: "Configure spaces",
    description: "Change who can read and write the group's spaces",
  },
  {
    id: "space.delete",
    label: "Delete spaces",
    description: "Delete the group's spaces",
  },
  {
    id: "community.configure",
    label: "Configure the group",
    description: "Edit the profile, roles and permissions",
  },
  {
    id: "mcp.configure",
    label: "Configure MCP servers",
    description: "Add, change and remove the group's MCP servers",
  },
];
