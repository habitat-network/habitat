import type { AuthManager } from "internal";
import { xrpc, type DidString } from "@atproto/lex";
import { queryOptions } from "@tanstack/react-query";
import { network } from "api";

export type SearchResult =
  network.habitat.space.searchRecords.$OutputBody["records"][number];

// searchRecordsQueryOptions full-text searches the space records the calling
// user can read in one org, on pear. Only the collections the org's admins
// surfaced in search (plus the defaults) are searched.
export function searchRecordsQueryOptions(
  authManager: AuthManager,
  org: DidString | undefined,
  q: string,
) {
  return queryOptions({
    queryKey: ["searchRecords", org, q],
    queryFn: async (): Promise<SearchResult[]> => {
      if (!org) return [];
      const response = await xrpc(
        authManager,
        network.habitat.space.searchRecords.main,
        { validateResponse: false, params: { q, org } },
      );
      return response.body.records;
    },
    enabled: !!org && q.trim() !== "",
  });
}
