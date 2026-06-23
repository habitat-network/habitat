import { queryOptions } from "@tanstack/react-query";
import { AuthManager, procedure, query } from "internal";

const DOCS_COLLECTION = "network.habitat.docs";
const DOCS_SPACE_TYPE = "network.habitat.docs";

// docsProxyHeaders targets the docs server via pear service proxying: pear
// validates the caller's OAuth session, signs a service-auth JWT, and forwards
// the network.habitat.doc.* call to the docs server's #docs service endpoint.
export function docsProxyHeaders(): Headers {
  return new Headers({ "Atproto-Proxy": `${__DOCS_SERVER_DID__}#docs` });
}

// The org's docs space. Its URI encodes the owner (org) DID as the second path
// segment: ats://<orgDid>/<type>/<skey>.
export const docsSpaceQueryOptions = (authManager: AuthManager) =>
  queryOptions({
    queryKey: ["docsSpace"],
    staleTime: 1000 * 60 * 5,
    queryFn: async () => {
      const { spaces } = await query(
        "network.habitat.space.listSpaces",
        { type: DOCS_SPACE_TYPE },
        { authManager },
      );
      const space = spaces[0];
      if (!space) {
        return undefined;
      }
      return { uri: space.uri, orgDid: space.uri.split("/")[2] };
    },
  });

export interface DocSummary {
  docId: string;
  name: string;
}

export const docsListQueryOptions = (authManager: AuthManager) =>
  queryOptions({
    queryKey: ["docs"],
    queryFn: async ({ client }): Promise<DocSummary[]> => {
      const space = await client.fetchQuery(docsSpaceQueryOptions(authManager));
      if (!space) {
        return [];
      }
      const { records } = await query(
        "network.habitat.space.listRecords",
        { space: space.uri, repo: space.orgDid, collection: DOCS_COLLECTION },
        { authManager },
      );
      return records.map((r): DocSummary => ({
        docId: r.rkey,
        name: (r.value as { name?: string }).name || "Untitled",
      }));
    },
  });

export const docQueryOptions = (docId: string, authManager: AuthManager) =>
  queryOptions({
    queryKey: ["doc", docId],
    queryFn: async ({ client }) => {
      const space = await client.fetchQuery(docsSpaceQueryOptions(authManager));
      if (!space) {
        throw new Error("docs space not found");
      }
      const record = await query(
        "network.habitat.space.getRecord",
        {
          space: space.uri,
          repo: space.orgDid,
          collection: DOCS_COLLECTION,
          rkey: docId,
        },
        { authManager },
      );
      return record.value as { name: string; blob?: string };
    },
  });

export function createDoc(authManager: AuthManager, name: string) {
  return procedure(
    "network.habitat.doc.createDoc",
    { name },
    { authManager, headers: docsProxyHeaders() },
  );
}

// pushUpdate sends a base64-encoded Yjs update through pear to the docs server,
// which merges it into the canonical document.
export function pushUpdate(
  authManager: AuthManager,
  docId: string,
  update: string,
) {
  return procedure(
    "network.habitat.doc.updateDoc",
    { docId, update },
    { authManager, headers: docsProxyHeaders() },
  );
}
