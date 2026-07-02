import type { AuthManager } from "internal";
import { query } from "internal";
import { queryOptions } from "@tanstack/react-query";
import type {
  CollectionView,
  RecordView,
} from "api/types/network/habitat/collections/defs";
import { homeProxyHeaders } from "./groups";

export type { CollectionView, RecordView };

// collectionsListQueryOptions lists the collections present in the org's synced
// data with a count of the records the calling user can see in each, as
// resolved by the home server's index.
export function collectionsListQueryOptions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["collections"],
    queryFn: async (): Promise<CollectionView[]> => {
      const { collections } = await query(
        "network.habitat.collections.listCollections",
        {},
        { authManager, headers: homeProxyHeaders() },
      );
      return collections;
    },
  });
}

// collectionRecordsQueryOptions lists the records in a collection the calling
// user can see, each with the spaces (they can read) it belongs to. Record
// bodies are fetched separately, on demand, from pear.
export function collectionRecordsQueryOptions(
  collection: string,
  authManager: AuthManager,
) {
  return queryOptions({
    queryKey: ["collection", collection],
    queryFn: async (): Promise<RecordView[]> => {
      const { records } = await query(
        "network.habitat.collections.listRecords",
        { collection },
        { authManager, headers: homeProxyHeaders() },
      );
      return records;
    },
  });
}

// recordBodyQueryOptions fetches a single record's body directly from pear, by
// one of the spaces it belongs to. The collections index never stores bodies.
export function recordBodyQueryOptions(
  record: RecordView,
  authManager: AuthManager,
) {
  const space = record.spaces[0];
  return queryOptions({
    queryKey: ["record-body", record.uri, space],
    enabled: space !== undefined,
    queryFn: async (): Promise<unknown> => {
      const { value } = await query(
        "network.habitat.space.getRecord",
        {
          space,
          repo: record.repo,
          collection: record.collection,
          rkey: record.rkey,
        },
        { authManager },
      );
      return value;
    },
  });
}
