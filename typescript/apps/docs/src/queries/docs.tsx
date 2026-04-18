import { HabitatDoc } from "@/habitatDoc";
import { mutationOptions, queryOptions } from "@tanstack/react-query";
import {
  AuthManager,
  getPrivateRecord,
  listPrivateRecords,
  procedure,
  query,
  TypedRecord,
} from "internal";

export const docsListQueryOptions = (authManager: AuthManager) =>
  queryOptions({
    queryKey: ["docs"],
    staleTime: 1000 * 60 * 5,
    queryFn: () =>
      listPrivateRecords<HabitatDoc>(authManager, "network.habitat.docs"),
  });

export const docQueryOptions = (uri: string, authManager: AuthManager) => {
  const [, , docDID, , rkey] = uri.split("/");
  return queryOptions({
    queryKey: ["doc", uri],
    queryFn: () =>
      getPrivateRecord<HabitatDoc>(
        authManager,
        "network.habitat.docs",
        rkey,
        docDID,
        true,
      ),
  });
};

export const docEditorsQueryOptions = (
  editorCliqueUri: string,
  authManager: AuthManager,
) =>
  queryOptions({
    queryKey: ["editors", editorCliqueUri],
    queryFn: async () => {
      const { members } = await query(
        "network.habitat.clique.getMembers",
        {
          clique: editorCliqueUri,
        },
        { authManager },
      );
      return members ?? [];
    },
  });

export const editorProfilesQueryOptions = (
  editorCliqueUri: string | undefined,
  authManager: AuthManager,
) =>
  queryOptions({
    queryKey: ["editors", editorCliqueUri, "profiles"],
    queryFn: async ({ client }) => {
      if (!editorCliqueUri) {
        return [];
      }
      const dids = await client.fetchQuery(
        docEditorsQueryOptions(editorCliqueUri, authManager),
      );
      if (!dids.length) {
        return [];
      }
      const { profiles } = await query(
        "app.bsky.actor.getProfiles",
        {
          actors: dids,
        },
        { authManager },
      );
      return profiles;
    },
  });

export const docEditsQueryOptions = (
  ownerRecord: TypedRecord<HabitatDoc>,
  authManager: AuthManager,
) =>
  queryOptions({
    queryKey: ["edits", ownerRecord.uri],
    queryFn: async ({ client }) => {
      if (!ownerRecord.value.editorClique) {
        return [];
      }
      const permissions = await client.fetchQuery(
        docEditorsQueryOptions(ownerRecord.value.editorClique, authManager),
      );
      if (!permissions) {
        return [];
      }

      const [, , ownerDID, , ownerRkey] = ownerRecord.uri.split("/");
      // editRkey is the rkey used in network.habitat.docs.edit for this doc
      const editRkey = `${ownerDID}-${ownerRkey}`;
      return Promise.all(
        permissions.map(async (did) => {
          try {
            const edit = await getPrivateRecord<HabitatDoc>(
              authManager,
              "network.habitat.docs.edit",
              editRkey,
              did,
            );
            return edit;
          } catch {
            /* silently skip */
          }
        }),
      );
    },
  });

export const deleteDocMutationOptions = (authManager: AuthManager) =>
  mutationOptions({
    mutationFn: async ({ uri }: { uri: string }) => {
      const [, , repo, , rkey] = uri.split("/");
      await procedure(
        "network.habitat.repo.deleteRecord",
        {
          repo,
          collection: "network.habitat.docs",
          rkey,
        },
        { authManager },
      );
    },
  });

export const addPermissionMutationOptions = (authManager: AuthManager) =>
  mutationOptions({
    mutationFn: async (
      {
        grantees,
        editorCliqueUri,
      }: {
        grantees: string[];
        editorCliqueUri: string | undefined;
      },
      { client },
    ) => {
      if (!editorCliqueUri) return;
      await procedure(
        "network.habitat.clique.addMembers",
        {
          clique: {
            $type: "network.habitat.grantee#clique",
            clique: editorCliqueUri,
          },
          members: grantees,
        },
        { authManager },
      );
      await client.invalidateQueries(
        docEditorsQueryOptions(editorCliqueUri, authManager),
      );
    },
  });

export const removePermissionMutationOptions = (authManager: AuthManager) =>
  mutationOptions({
    mutationFn: async (
      {
        grantee,
        editorCliqueUri,
      }: {
        grantee: string;
        editorCliqueUri: string | undefined;
      },
      { client },
    ) => {
      if (!editorCliqueUri) return;
      await procedure(
        "network.habitat.clique.removeMembers",
        {
          clique: {
            $type: "network.habitat.grantee#clique",
            clique: editorCliqueUri,
          },
          members: [grantee],
        },
        { authManager },
      );
      await client.invalidateQueries(
        docEditorsQueryOptions(editorCliqueUri, authManager),
      );
    },
  });
