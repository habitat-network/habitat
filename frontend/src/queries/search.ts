import type { AuthManager } from "internal";
import { xrpc } from "@atproto/lex";
import { queryOptions } from "@tanstack/react-query";
import { network } from "api";

export type SearchResult =
  network.habitat.space.searchRecords.$OutputBody["records"][number];

// searchRecordsQueryOptions full-text searches the space records the calling
// user can read, on pear.
export function searchRecordsQueryOptions(authManager: AuthManager, q: string) {
  return queryOptions({
    queryKey: ["searchRecords", q],
    queryFn: async (): Promise<SearchResult[]> => {
      const response = await xrpc(
        authManager,
        network.habitat.space.searchRecords.main,
        { validateResponse: false, params: { q } },
      );
      return response.body.records;
    },
    enabled: q.trim() !== "",
  });
}
