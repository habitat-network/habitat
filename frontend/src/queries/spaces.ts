import type { AuthManager } from "internal";
import { query } from "internal";
import { queryOptions } from "@tanstack/react-query";
import type { SpaceView } from "api/types/network/habitat/space/listSpaces";
import type { Repo } from "api/types/network/habitat/space/listRepos";
import type { Record as SpaceRecord } from "api/types/network/habitat/space/listRecords";

export type { SpaceView, Repo, SpaceRecord };

// The list lexicons declare limit/cursor, but the space host does not paginate
// yet: it returns the complete set and never a cursor, and listRepos answers
// 501 if limit or cursor is sent at all. So none of these pass pagination
// params. Revisit when the server grows real pagination.

// SpacesFilter narrows a listing to one owner and/or one space type. Both are
// applied server-side by listSpaces; an empty filter lists everything the
// calling user participates in.
export interface SpacesFilter {
  did?: string;
  type?: string;
}

// spacesListQueryOptions lists the spaces the calling user participates in,
// optionally narrowed by owner DID and/or type. Each filter combination is
// cached under its own key.
export function spacesListQueryOptions(
  authManager: AuthManager,
  filter: SpacesFilter = {},
) {
  return queryOptions({
    queryKey: ["listSpaces", filter.did ?? null, filter.type ?? null],
    queryFn: async (): Promise<SpaceView[]> => {
      const { spaces } = await query(
        "network.habitat.space.listSpaces",
        { did: filter.did, type: filter.type },
        { authManager },
      );
      return spaces;
    },
  });
}

// spaceReposQueryOptions lists the repos holding data in a space — the space's
// members, from the authority's point of view.
export function spaceReposQueryOptions(
  space: string,
  authManager: AuthManager,
) {
  return queryOptions({
    queryKey: ["listRepos", space],
    queryFn: async (): Promise<Repo[]> => {
      const { repos } = await query(
        "network.habitat.space.listRepos",
        { space },
        { authManager },
      );
      return repos;
    },
  });
}

// spaceRecordsQueryOptions lists one member's records in a space. Values are
// excluded: the member page only groups records by collection, and each
// record's body is fetched on demand by the record page.
export function spaceRecordsQueryOptions(
  space: string,
  repo: string,
  authManager: AuthManager,
) {
  return queryOptions({
    queryKey: ["listRecords", space, repo],
    queryFn: async (): Promise<SpaceRecord[]> => {
      const { records } = await query(
        "network.habitat.space.listRecords",
        { space, repo, excludeValues: true },
        { authManager },
      );
      return records;
    },
  });
}

// spaceRecordQueryOptions fetches a single record's body for the JSON viewer.
export function spaceRecordQueryOptions(
  {
    space,
    repo,
    collection,
    rkey,
  }: { space: string; repo: string; collection: string; rkey: string },
  authManager: AuthManager,
) {
  return queryOptions({
    queryKey: ["getRecord", space, repo, collection, rkey],
    queryFn: async (): Promise<{ value: unknown; cid?: string }> => {
      const { value, cid } = await query(
        "network.habitat.space.getRecord",
        { space, repo, collection, rkey },
        { authManager },
      );
      return { value, cid };
    },
  });
}
