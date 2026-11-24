import { queryOptions } from "@tanstack/react-query";
import { listFiles } from "@/server/fns/files";

// Files: a group's uploads.
// Query options are shared between route loaders (which ensure them) and
// the components that read them, so both use the same cache entries.

export const filesQuery = (org: string) =>
  queryOptions({
    queryKey: ["files", org],
    queryFn: () => listFiles({ data: { org } }),
  });
