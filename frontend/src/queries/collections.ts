import type { AuthManager } from "internal";
import {
  xrpc,
  type AtUriString,
  type DidString,
  type NsidString,
} from "@atproto/lex";
import { queryOptions } from "@tanstack/react-query";
import { network } from "api";
import { homeProxyHeaders } from "./groups";

export type CollectionView =
  network.habitat.collections.listCollections.$OutputBody["collections"][number];

export type RecordView =
  network.habitat.collections.listRecords.$OutputBody["records"][number];

// collectionsListQueryOptions lists the collections present in the org's synced
// data with a count of the records the calling user can see in each, as
// resolved by the home server's index.
export function collectionsListQueryOptions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["collections"],
    queryFn: async (): Promise<CollectionView[]> => {
      const response = await xrpc(
        authManager,
        network.habitat.collections.listCollections.main,
        { params: {}, headers: homeProxyHeaders() },
      );
      return response.body.collections;
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
      const response = await xrpc(
        authManager,
        network.habitat.collections.listRecords.main,
        {
          params: { collection: collection as NsidString },
          headers: homeProxyHeaders(),
        },
      );
      return response.body.records;
    },
  });
}

// recordBodyQueryOptions fetches a single record's body directly from pear,
// from the space it belongs to. The collections index never stores bodies.
export function recordBodyQueryOptions(
  record: RecordView,
  authManager: AuthManager,
) {
  return queryOptions({
    queryKey: ["record-body", record.uri],
    queryFn: async (): Promise<unknown> => {
      const response = await xrpc(
        authManager,
        network.habitat.space.getRecord.main,
        {
          params: {
            space: record.space as AtUriString,
            repo: record.repo as DidString,
            collection: record.collection as NsidString,
            rkey: record.rkey,
          },
        },
      );
      return response.body.value as unknown;
    },
  });
}
