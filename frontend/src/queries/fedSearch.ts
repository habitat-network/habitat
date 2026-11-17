import type { AuthManager } from "internal";
import { xrpc, type DidString } from "@atproto/lex";
import { infiniteQueryOptions } from "@tanstack/react-query";
import { network } from "api";
import { pearAgent } from "./pearAgent";

export type SearchPage = network.habitat.mcp.search.$OutputBody;
export type SearchResult = network.habitat.mcp.search.Result;
export type SearchBackend = network.habitat.mcp.search.Backend;

// ALL_SOURCES selects the mixed stream: every source's results ranked
// together.
export const ALL_SOURCES = "all";

// fedSearchQueryOptions pages through one stream of a federated search over
// the org's Habitat records and the caller's connected apps: the mixed
// stream for ALL_SOURCES, or one source's own results. The mixed stream
// starts the search with q; a source's stream starts from the cursor the
// mixed stream's first page gave that source (sourceCursor), so it waits
// for that source if it's still running. Each call waits up to 10s on the
// server for results.
//
// mcp.search requires service-auth, so it's proxied to the org's own habitat
// instance via pearAgent.
export function fedSearchQueryOptions(
  authManager: AuthManager,
  org: DidString,
  q: string,
  source: string,
  sourceCursor: string | undefined,
) {
  const mixed = source === ALL_SOURCES;
  return infiniteQueryOptions({
    queryKey: ["fedSearch", org, q, source],
    queryFn: async ({ pageParam }): Promise<SearchPage> => {
      const response = await xrpc(
        pearAgent(authManager, `${org}#habitat`),
        network.habitat.mcp.search.main,
        {
          validateResponse: false,
          body: pageParam ? { org, cursor: pageParam } : { org, query: q },
        },
      );
      return response.body;
    },
    initialPageParam: mixed ? undefined : sourceCursor,
    getNextPageParam: (last) => last.cursor,
    enabled: q.trim() !== "" && (mixed || !!sourceCursor),
    // A search's pages are fixed once served; refetching would start over.
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });
}
