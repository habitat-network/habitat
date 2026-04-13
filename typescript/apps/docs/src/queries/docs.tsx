import { HabitatDoc } from "@/habitatDoc";
import { mutationOptions, queryOptions } from "@tanstack/react-query";
import {
  AuthManager,
  getPrivateRecord,
  getPublicRecord,
  listPrivateRecords,
  listPublicRecords,
  procedure,
  query,
  TypedRecord,
} from "internal";

const isPublicUri = (uri: string) => uri.startsWith("at://");

const getDoc = (
  authManager: AuthManager,
  uri: string,
  includePermissions?: boolean,
): Promise<TypedRecord<HabitatDoc>> => {
  const [, , did, , rkey] = uri.split("/");
  if (isPublicUri(uri)) {
    return getPublicRecord<HabitatDoc>(authManager, "network.habitat.docs", rkey, did);
  }
  return getPrivateRecord<HabitatDoc>(
    authManager,
    "network.habitat.docs",
    rkey,
    did,
    includePermissions,
  );
};

const deleteDoc = async (authManager: AuthManager, uri: string): Promise<void> => {
  const [, , repo, , rkey] = uri.split("/");
  if (isPublicUri(uri)) {
    await procedure(
      "com.atproto.repo.deleteRecord",
      { repo, collection: "network.habitat.docs", rkey },
      { authManager },
    );
  } else {
    await procedure(
      "network.habitat.repo.deleteRecord",
      { repo, collection: "network.habitat.docs", rkey },
      { authManager },
    );
  }
};

export const docsListQueryOptions = (authManager: AuthManager) =>
  queryOptions({
    queryKey: ["docs"],
    staleTime: 1000 * 60 * 5,
    queryFn: async () => {
      const did = authManager.getAuthInfo()?.did;
      const [privateResult, publicResult] = await Promise.all([
        listPrivateRecords<HabitatDoc>(authManager, "network.habitat.docs"),
        did
          ? listPublicRecords<HabitatDoc>(authManager, "network.habitat.docs", did)
          : Promise.resolve({ records: [] as TypedRecord<HabitatDoc>[] }),
      ]);
      return { records: [...privateResult.records, ...publicResult.records] };
    },
  });

export const docQueryOptions = (uri: string, authManager: AuthManager) =>
  queryOptions({
    queryKey: ["doc", uri],
    queryFn: () => getDoc(authManager, uri, true),
  });

export const docEditorsQueryOptions = (
  editorCliqueUri: string,
  authManager: AuthManager,
) =>
  queryOptions({
    queryKey: ["editors", editorCliqueUri],
    queryFn: async () => {
      const { members } = await query(
        "network.habitat.clique.getMembers",
        { clique: editorCliqueUri },
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
        { actors: dids },
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
      const editRkey = `${ownerDID}-${ownerRkey}`;
      return Promise.all(
        permissions.map(async (did) => {
          try {
            return await getPrivateRecord<HabitatDoc>(
              authManager,
              "network.habitat.docs.edit",
              editRkey,
              did,
            );
          } catch {
            /* silently skip */
          }
        }),
      );
    },
  });

export const deleteDocMutationOptions = (authManager: AuthManager) =>
  mutationOptions({
    mutationFn: ({ uri }: { uri: string }) => deleteDoc(authManager, uri),
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

export const makePublicMutationOptions = (authManager: AuthManager) =>
  mutationOptions({
    mutationFn: async ({ uri, doc }: { uri: string; doc: HabitatDoc }) => {
      const [, , repo, , rkey] = uri.split("/");
      await procedure(
        "com.atproto.repo.putRecord",
        {
          repo,
          collection: "network.habitat.docs",
          rkey,
          record: { ...doc, isPublic: true },
        },
        { authManager },
      );
      await deleteDoc(authManager, uri);
      return `at://${repo}/network.habitat.docs/${rkey}`;
    },
  });
