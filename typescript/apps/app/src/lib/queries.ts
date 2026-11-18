import { queryOptions } from "@tanstack/react-query";
import { listMyGroups, getGroup, listMyInvites } from "@/server/fns/groups";

// Query options shared between route loaders (which prefetch) and the
// components that read them, so both use the same cache entries.

export const myGroupsQuery = queryOptions({
  queryKey: ["groups"],
  queryFn: () => listMyGroups(),
});

export const groupQuery = (org: string) =>
  queryOptions({
    queryKey: ["group", org],
    queryFn: () => getGroup({ data: { org } }),
  });

export const myInvitesQuery = queryOptions({
  queryKey: ["invites"],
  queryFn: () => listMyInvites(),
});

// groupLabel is what to call a group: its name, else its DID.
export function groupLabel(group: { did: string; name: string | null }) {
  return group.name?.trim() || group.did;
}
