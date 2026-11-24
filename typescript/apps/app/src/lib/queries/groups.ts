import { queryOptions } from "@tanstack/react-query";
import {
  getGroup,
  getMyGroupProfile,
  listMyGroups,
  listMyInvites,
} from "@/server/fns/groups";

// Groups: the member's groups, invites and profiles.
// Query options are shared between route loaders (which ensure them) and
// the components that read them, so both use the same cache entries.

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

export const myGroupProfileQuery = (org: string) =>
  queryOptions({
    queryKey: ["myGroupProfile", org],
    queryFn: () => getMyGroupProfile({ data: { org } }),
  });

// groupLabel is what to call a group: its name, else its DID.
export function groupLabel(group: { did: string; name: string | null }) {
  return group.name?.trim() || group.did;
}
