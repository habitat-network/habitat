import { queryOptions } from "@tanstack/react-query";
import { listMyGroups, getGroup, listMyInvites } from "@/server/fns/groups";
import { getOrgMcpUrl, listMcpServers, listMcpTools } from "@/server/fns/mcp";

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

export const mcpServersQuery = (org: string) =>
  queryOptions({
    queryKey: ["mcp", org],
    queryFn: () => listMcpServers({ data: { org } }),
  });

export const orgMcpUrlQuery = (org: string) =>
  queryOptions({
    queryKey: ["mcp", org, "url"],
    queryFn: () => getOrgMcpUrl({ data: { org } }),
  });

export const mcpToolsQuery = (org: string, id: string) =>
  queryOptions({
    queryKey: ["mcp", org, id, "tools"],
    queryFn: () => listMcpTools({ data: { org, id } }),
  });

// groupLabel is what to call a group: its name, else its DID.
export function groupLabel(group: { did: string; name: string | null }) {
  return group.name?.trim() || group.did;
}
