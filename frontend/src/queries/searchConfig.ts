import type { AuthManager } from "internal";
import { xrpc, type DidString } from "@atproto/lex";
import { mutationOptions, queryOptions } from "@tanstack/react-query";
import { network } from "api";
import { pearAgent } from "./pearAgent";

export const searchCollectionsQueryKey = (org: DidString) =>
  ["search", "collections", org] as const;

// orgSearchCollectionsQueryOptions lists the collections org surfaces in
// search results: those its admins added, and the defaults every org
// includes. Requires the caller to hold the community.configure action, so
// only enable it for admins. Requires service-auth, so it's proxied to the
// org's own habitat instance via pearAgent.
export function orgSearchCollectionsQueryOptions(
  org: DidString,
  authManager: AuthManager,
  enabled: boolean,
) {
  return queryOptions({
    queryKey: searchCollectionsQueryKey(org),
    enabled,
    queryFn: async () => {
      const response = await xrpc(
        pearAgent(authManager, `${org}#habitat`),
        network.habitat.search.listCollections.main,
        { params: { org } },
      );
      return response.body;
    },
  });
}

// addSearchCollectionMutationOptions surfaces a collection (an NSID) in org's
// search results. Adding one that's already configured is a no-op. Refetches
// the list before resolving, so isPending covers the refresh.
export const addSearchCollectionMutationOptions = (
  authManager: AuthManager,
  org: DidString,
) =>
  mutationOptions({
    mutationFn: async (
      collection: network.habitat.search.addCollection.$InputBody["collection"],
      { client },
    ) => {
      await xrpc(
        pearAgent(authManager, `${org}#habitat`),
        network.habitat.search.addCollection.main,
        { body: { org, collection } },
      );
      await client.invalidateQueries({
        queryKey: searchCollectionsQueryKey(org),
      });
    },
  });

// removeSearchCollectionMutationOptions stops surfacing an admin-added
// collection in org's search results. Defaults can't be removed.
export const removeSearchCollectionMutationOptions = (
  authManager: AuthManager,
  org: DidString,
) =>
  mutationOptions({
    mutationFn: async (
      collection: network.habitat.search.removeCollection.$InputBody["collection"],
      { client },
    ) => {
      await xrpc(
        pearAgent(authManager, `${org}#habitat`),
        network.habitat.search.removeCollection.main,
        { body: { org, collection } },
      );
      await client.invalidateQueries({
        queryKey: searchCollectionsQueryKey(org),
      });
    },
  });
