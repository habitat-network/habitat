import type { AuthManager } from "internal";
import {
  xrpc,
  XrpcResponseError,
  type DidString,
  type NsidString,
  type SpaceRefString,
} from "@atproto/lex";
import { queryOptions, type QueryClient } from "@tanstack/react-query";
import { com } from "api";
import { spaceAgent, spaceCredentialQueryOptions } from "./spaceCredential";

export type SpaceView =
  com.atproto.space.listSpaces.$OutputBody["spaces"][number];

export type Repo = com.atproto.space.listRepos.$OutputBody["repos"][number];

export type SpaceRecord =
  com.atproto.space.listRecords.$OutputBody["records"][number];

export type Member =
  com.atproto.simplespace.listMembers.$OutputBody["members"][number];

// The list lexicons declare limit/cursor, but the space host does not paginate
// yet: it returns the complete set and never a cursor, and listRepos answers
// 501 if limit or cursor is sent at all. So none of these pass pagination
// params. Revisit when the server grows real pagination.

// Mutations invalidate these queries and then call router.invalidate(). The
// loaders read through fetchQuery, which refetches an invalidated query, so
// re-running the loader is what puts the request on the wire.

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
      const response = await xrpc(
        authManager,
        com.atproto.space.listSpaces.main,
        {
          validateResponse: false,
          params: {
            did: filter.did as DidString | undefined,
            type: filter.type as NsidString | undefined,
          },
        },
      );
      return response.body.spaces;
    },
  });
}

// spaceReposQueryOptions lists the repos holding data in a space — the space's
// members, from the authority's point of view. Read via a space credential
// (see spaceCredentialQueryOptions), routed to the space's own resolved host.
export function spaceReposQueryOptions(
  space: string,
  authManager: AuthManager,
  queryClient: QueryClient,
) {
  return queryOptions({
    queryKey: ["listRepos", space],
    queryFn: async (): Promise<Repo[]> => {
      const cred = await queryClient.fetchQuery(
        spaceCredentialQueryOptions(space, authManager),
      );
      const response = await xrpc(
        spaceAgent(cred),
        com.atproto.space.listRepos.main,
        {
          validateResponse: false,
          params: { space: space as SpaceRefString },
        },
      );
      return response.body.repos;
    },
  });
}

// SpaceCommit is the decoded shape of com.atproto.space.defs#signedCommit.
// xrpc already runs responses through lex decoding, so byte fields arrive as
// Uint8Array, matching the generated lexicon type.
export interface SpaceCommit {
  ver: number;
  rev: string;
  hash: Uint8Array;
  ikm: Uint8Array;
  mac: Uint8Array;
  sig: Uint8Array;
}

// spaceLatestCommitQueryOptions fetches the host-signed commit over a repo's
// current state in a space. A repo that holds no records has no commit to sign
// and the host answers RepoNotFound, which is a normal state here rather than
// an error, so it resolves to null. Read via a space credential (see
// spaceCredentialQueryOptions), routed to the space's own resolved host.
export function spaceLatestCommitQueryOptions(
  space: string,
  repo: string,
  authManager: AuthManager,
  queryClient: QueryClient,
) {
  return queryOptions({
    queryKey: ["getLatestCommit", space, repo],
    queryFn: async (): Promise<SpaceCommit | null> => {
      try {
        const cred = await queryClient.fetchQuery(
          spaceCredentialQueryOptions(space, authManager),
        );
        const response = await xrpc(
          spaceAgent(cred),
          com.atproto.space.getLatestCommit.main,
          {
            validateResponse: false,
            params: { space: space as SpaceRefString, repo: repo as DidString },
          },
        );
        return response.body.commit ?? null;
      } catch (err) {
        if (err instanceof XrpcResponseError && err.error === "RepoNotFound") {
          return null;
        }
        throw err;
      }
    },
  });
}

// spaceMembersQueryOptions lists a space's member list — the DIDs granted read
// access. This is the space's ACL, which is broader than the writer set
// listRepos returns: a member who has never written data has no repo.
// The canonical com.atproto.simplespace.listMembers member type carries read
// and write access, but the host has not grown those yet and still answers
// with { did } only, so the response is taken unvalidated. Relax when the
// host starts returning read/write.
export function spaceMembersQueryOptions(
  space: string,
  authManager: AuthManager,
) {
  return queryOptions({
    queryKey: ["listMembers", space],
    queryFn: async (): Promise<Member[]> => {
      const response = await xrpc(
        authManager,
        com.atproto.simplespace.listMembers.main,
        {
          params: { space: space as SpaceRefString },
          validateResponse: false,
        },
      );
      return response.body.members;
    },
  });
}

// spaceRecordsQueryOptions lists one member's records in a space. Values are
// excluded: the member page only groups records by collection, and each
// record's body is fetched on demand by the record page. Read via a space
// credential (see spaceCredentialQueryOptions), routed to the space's own
// resolved host.
export function spaceRecordsQueryOptions(
  space: string,
  repo: string,
  authManager: AuthManager,
  queryClient: QueryClient,
) {
  return queryOptions({
    queryKey: ["listRecords", space, repo],
    queryFn: async (): Promise<SpaceRecord[]> => {
      const cred = await queryClient.fetchQuery(
        spaceCredentialQueryOptions(space, authManager),
      );
      const response = await xrpc(
        spaceAgent(cred),
        com.atproto.space.listRecords.main,
        {
          validateResponse: false,
          params: {
            space: space as SpaceRefString,
            repo: repo as DidString,
            excludeValues: true,
          },
        },
      );
      return response.body.records;
    },
  });
}

// spaceRecordQueryOptions fetches a single record's body for the JSON viewer.
// Read via a space credential (see spaceCredentialQueryOptions), routed to the
// space's own resolved host.
export function spaceRecordQueryOptions(
  {
    space,
    repo,
    collection,
    rkey,
  }: { space: string; repo: string; collection: string; rkey: string },
  authManager: AuthManager,
  queryClient: QueryClient,
) {
  return queryOptions({
    queryKey: ["getRecord", space, repo, collection, rkey],
    queryFn: async (): Promise<{ value: unknown; cid?: string }> => {
      const cred = await queryClient.fetchQuery(
        spaceCredentialQueryOptions(space, authManager),
      );
      const response = await xrpc(
        spaceAgent(cred),
        com.atproto.space.getRecord.main,
        {
          params: {
            space: space as SpaceRefString,
            repo: repo as DidString,
            collection: collection as NsidString,
            rkey,
          },
        },
      );
      return { value: response.body.value as unknown, cid: response.body.cid };
    },
  });
}
