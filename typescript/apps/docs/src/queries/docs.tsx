import { HabitatDoc } from "@/habitatDoc";
import { parseSpaceRecordUri } from "@/utils";
import { mutationOptions, queryOptions } from "@tanstack/react-query";
import {
  AuthManager,
  getProfiles,
  procedure,
  query,
  TypedRecord,
} from "internal";

export const docsListQueryOptions = (authManager: AuthManager) =>
  queryOptions({
    queryKey: ["docs"],
    staleTime: 1000 * 60 * 5,
    queryFn: async ({ client }) => {
      const { spaces } = await query(
        "network.habitat.space.listSpaces",
        { type: "network.habitat.docs" },
        { authManager },
      );
      const records = await Promise.all(
        spaces.map(async (space) => {
          try {
            const record = await client.ensureQueryData(
              ownerDocQueryOptions(space.uri, authManager),
            );
            return record;
          } catch {
            return null;
          }
        }),
      );
      return { records: records.filter(Boolean) as TypedRecord<HabitatDoc>[] };
    },
  });

export const ownerDocQueryOptions = (
  spaceUri: string,
  authManager: AuthManager,
) =>
  queryOptions({
    queryKey: ["owner-doc", spaceUri],
    queryFn: async () => {
      const { spaceOwner } = parseSpaceRecordUri(spaceUri);
      const { records } = await query(
        "network.habitat.space.listRecords",
        {
          space: spaceUri,
          collection: "network.habitat.docs.edit",
          repo: spaceOwner,
        },
        { authManager },
      );
      if (!records.length) {
        return null;
      }
      // each the owner should have exactly one network.habitat.docs.edit
      const { rkey, value, cid } = records[0];
      return {
        uri: `${spaceUri}/${spaceOwner}/network.habitat.docs.edit/${rkey}`,
        cid: cid,
        value: value as HabitatDoc,
      };
    },
  });

export const docEditorsQueryOptions = (
  spaceUri: string,
  authManager: AuthManager,
) =>
  queryOptions({
    queryKey: ["editors", spaceUri],
    queryFn: async () => {
      const { members } = await query(
        "network.habitat.space.getMembers",
        { space: spaceUri },
        { authManager },
      );
      return (members ?? []).map((m) => m.did);
    },
  });

export const editorProfilesQueryOptions = (
  spaceUri: string | undefined,
  authManager: AuthManager,
) =>
  queryOptions({
    queryKey: ["editors", spaceUri, "profiles"],
    queryFn: async ({ client }) => {
      if (!spaceUri) {
        return [];
      }
      const dids = await client.fetchQuery(
        docEditorsQueryOptions(spaceUri, authManager),
      );
      if (!dids.length) {
        return [];
      }
      const profiles = await getProfiles(dids);
      return profiles;
    },
  });

export const docEditQueryOptions = (
spaceUri: string ,
repo: string,
  authManager: AuthManager,
) => queryOptions({
    queryKey: ["edit", spaceUri, repo],
    queryFn: async () => {
      const { records } = await query(
        "network.habitat.space.listRecords",
        {
          space: spaceUri,
          collection: "network.habitat.docs.edit",
          repo: repo,
        },
        { authManager },
      );
      const { rkey, value, cid } = records[0];
      return {
        uri: `${spaceUri}/${repo}/network.habitat.docs.edit/${rkey}`,
        cid: cid,
        value: value as HabitatDoc,
      };
    },
  });

export const docEditsQueryOptions = (
  spaceUri: string,
  authManager: AuthManager,
) =>
  queryOptions({
    queryKey: ["edits", spaceUri],
    queryFn: async ({ client}) => {
        const dids = await client.fetchQuery(
          docEditorsQueryOptions(spaceUri, authManager),
        );
        if (!dids.length) {
          return [];
        }
        const records = await Promise.all(
          dids.map((did) =>  (
          client.fetchQuery(
              docEditQueryOptions(spaceUri, did, authManager),
            )
          )
        ))
        return records;
      } 
  });

export const deleteDocMutationOptions = (authManager: AuthManager) =>
  mutationOptions({
    mutationFn: async ({ uri }: { uri: string }) => {
      // uri format: ats://did:plc:owner/network.habitat.docs/skey/network.habitat.docs/doc
      const parts = uri.split("/");
      const spaceUri = `ats://${parts[2]}/network.habitat.docs/${parts[4]}`;
      await procedure(
        "network.habitat.space.deleteRecord",
        { space: spaceUri, collection: "network.habitat.docs", rkey: "doc" },
        { authManager },
      );
    },
  });

export const addPermissionMutationOptions = (authManager: AuthManager) =>
  mutationOptions({
    mutationFn: async (
      {
        grantees,
        spaceUri,
      }: { grantees: string[]; spaceUri: string | undefined },
      { client },
    ) => {
      if (!spaceUri) return;
      await Promise.all(
        grantees.map((did) =>
          procedure(
            "network.habitat.space.addMember",
            { space: spaceUri, did },
            { authManager },
          ),
        ),
      );
      await client.invalidateQueries(
        docEditorsQueryOptions(spaceUri, authManager),
      );
    },
  });

export const removePermissionMutationOptions = (authManager: AuthManager) =>
  mutationOptions({
    mutationFn: async (
      { grantee, spaceUri }: { grantee: string; spaceUri: string | undefined },
      { client },
    ) => {
      if (!spaceUri) return;
      await procedure(
        "network.habitat.space.removeMember",
        { space: spaceUri, did: grantee },
        { authManager },
      );
      await client.invalidateQueries(
        docEditorsQueryOptions(spaceUri, authManager),
      );
    },
  });
