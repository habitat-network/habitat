import { DocRecord } from "@/habitatDoc";
import { NetworkHabitatDocs, NetworkHabitatDocsEdit } from "api";
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

const getDoc = async (
  authManager: AuthManager,
  uri: string,
  includePermissions?: boolean,
): Promise<DocRecord> => {
  const [, , did, , rkey] = uri.split("/");
  const isPublic = isPublicUri(uri);
  const record = isPublic
    ? await getPublicRecord<NetworkHabitatDocs.Main>(authManager, "network.habitat.docs", rkey, did)
    : await getPrivateRecord<NetworkHabitatDocs.Main>(authManager, "network.habitat.docs", rkey, did, includePermissions);
  return { ...record, isPublic };
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
        listPrivateRecords<NetworkHabitatDocs.Main>(authManager, "network.habitat.docs"),
        did
          ? listPublicRecords<NetworkHabitatDocs.Main>(authManager, "network.habitat.docs", did)
          : Promise.resolve({ records: [] as TypedRecord<NetworkHabitatDocs.Main>[] }),
      ]);
      const records: DocRecord[] = [
        ...privateResult.records.map((r) => ({ ...r, isPublic: false })),
        ...publicResult.records.map((r) => ({ ...r, isPublic: true })),
      ];
      return { records };
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
  ownerRecord: DocRecord,
  authManager: AuthManager,
) =>
  queryOptions({
    queryKey: ["edits", ownerRecord.uri],
    queryFn: async ({ client }) => {
      if (isPublicUri(ownerRecord.uri)) {
        // Fetch backlinks from constellation
        const params = new URLSearchParams({ subject: ownerRecord.uri, source: "network.habitat.docs.edit:doc" });
        const resp = await fetch(
          `https://constellation.microcosm.blue/xrpc/blue.microcosm.links.getBacklinks?${params}`,
        );
        if (!resp.ok) return [];
        const data = await resp.json() as { records?: { did: string; collection: string; rkey: string }[] };
        const records = data.records ?? [];
        return Promise.all(
          records.map(async ({ did, rkey }) => {
            try {
              return await getPublicRecord<NetworkHabitatDocsEdit.Main>(
                authManager,
                "network.habitat.docs.edit",
                rkey,
                did,
              );
            } catch {
              /* silently skip */
            }
          }),
        );
      }

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
            return await getPrivateRecord<NetworkHabitatDocsEdit.Main>(
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
    mutationFn: async ({ uri, doc }: { uri: string; doc: NetworkHabitatDocs.Main }) => {
      const [, , repo, , rkey] = uri.split("/");
      const record = { ...doc, isPublic: true }
      delete record["editorClique"];
      await procedure(
        "com.atproto.repo.putRecord",
        {
          repo,
          collection: "network.habitat.docs",
          rkey,
          record,
        },
        { authManager },
      );
      await deleteDoc(authManager, uri);
      return `at://${repo}/network.habitat.docs/${rkey}`;
    },
  });
