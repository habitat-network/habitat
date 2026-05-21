import { HabitatDoc } from "@/habitatDoc";
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
    queryFn: async () => {
      const { spaces } = await query(
        "network.habitat.space.listSpaces",
        { type: "network.habitat.docs" },
        { authManager },
      );
      const records = await Promise.all(
        spaces.map(async (space) => {
          try {
            const rec = await query(
              "network.habitat.space.getRecord",
              {
                space: space.uri,
                collection: "network.habitat.docs",
                rkey: "doc",
              },
              { authManager },
            );
            return {
              uri: rec.uri,
              cid: rec.cid,
              value: rec.value as HabitatDoc,
            };
          } catch {
            return null;
          }
        }),
      );
      return { records: records.filter(Boolean) as TypedRecord<HabitatDoc>[] };
    },
  });

export const docQueryOptions = (uri: string, authManager: AuthManager) =>
  queryOptions({
    queryKey: ["doc", uri],
    queryFn: async () => {
      const parts = uri.split("/");
      const spaceDID = parts[2];
      const spaceSkey = parts[4];
      const spaceURI = `ats://${spaceDID}/network.habitat.docs/${spaceSkey}`;
      const rec = await query(
        "network.habitat.space.getRecord",
        { space: spaceURI, collection: "network.habitat.docs", rkey: "doc" },
        { authManager },
      );
      return { uri: rec.uri, cid: rec.cid, value: rec.value as HabitatDoc, space: spaceURI } as TypedRecord<HabitatDoc> & { space: string };
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

export const docEditsQueryOptions = (
  spaceUri: string,
  authManager: AuthManager,
) =>
  queryOptions({
    queryKey: ["edits", spaceUri],
    queryFn: async () => {
      try {
        const { records } = await query(
          "network.habitat.space.listRecords",
          { space: spaceUri, collection: "network.habitat.docs.edit" },
          { authManager },
        );
        return records.map((r) => ({
          uri: `${spaceUri}/network.habitat.docs.edit/${r.rkey}`,
          cid: r.cid,
          value: r as unknown as HabitatDoc,
        }));
      } catch {
        return [];
      }
    },
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
      { grantees, spaceUri }: { grantees: string[]; spaceUri: string | undefined },
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
