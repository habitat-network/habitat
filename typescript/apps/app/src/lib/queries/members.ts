import { queryOptions } from "@tanstack/react-query";
import {
  getRolesAndPermissions,
  listGroupInvites,
  listGroupMembers,
} from "@/server/fns/manage";

// Members: a group's roster, outstanding invites and roles.
// Query options are shared between route loaders (which ensure them) and
// the components that read them, so both use the same cache entries.

export const membersQuery = (org: string) =>
  queryOptions({
    queryKey: ["members", org],
    queryFn: () => listGroupMembers({ data: { org } }),
  });

export const groupInvitesQuery = (org: string) =>
  queryOptions({
    queryKey: ["groupInvites", org],
    queryFn: () => listGroupInvites({ data: { org } }),
  });

export const rolesQuery = (org: string) =>
  queryOptions({
    queryKey: ["roles", org],
    queryFn: () => getRolesAndPermissions({ data: { org } }),
  });
