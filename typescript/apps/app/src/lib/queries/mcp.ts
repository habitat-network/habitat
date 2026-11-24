import { queryOptions } from "@tanstack/react-query";
import { listConnectedApps, listMcpServers } from "@/server/fns/mcp";

// MCP: the group's servers and the apps a member has authorized.
// Query options are shared between route loaders (which ensure them) and
// the components that read them, so both use the same cache entries.

export const mcpServersQuery = (org: string) =>
  queryOptions({
    queryKey: ["mcp", org],
    queryFn: () => listMcpServers({ data: { org } }),
  });

export const connectedAppsQuery = queryOptions({
  queryKey: ["connectedApps"],
  queryFn: () => listConnectedApps(),
});
