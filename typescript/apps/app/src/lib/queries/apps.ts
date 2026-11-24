import { queryOptions } from "@tanstack/react-query";
import { listApps } from "@/server/fns/apps";

// Apps: code a group has deployed.
// Query options are shared between route loaders (which ensure them) and
// the components that read them, so both use the same cache entries.

export const appsQuery = (org: string) =>
  queryOptions({
    queryKey: ["apps", org],
    queryFn: () => listApps({ data: { org } }),
  });
