import { queryOptions } from "@tanstack/react-query";
import { AuthManager, procedure, query } from "internal";

const CRDT_COLLECTION = "network.habitat.docs.crdt";
const SELF = "self";

// docsProxyHeaders targets the docs server via pear service proxying: pear
// validates the caller's OAuth session, signs a service-auth JWT, and forwards
// the network.habitat.doc.* call to the docs server's #docs service endpoint.
export function docsProxyHeaders(): Headers {
  return new Headers({ "Atproto-Proxy": `${__DOCS_SERVER_DID__}#docs` });
}

export interface DocSummary {
  docId: string;
  uri: string;
  title: string;
}

// The sidebar list comes entirely from the docs server's listDocs endpoint,
// which enumerates the org's doc spaces and reads each one's markdown title.
export const docsListQueryOptions = (authManager: AuthManager) =>
  queryOptions({
    queryKey: ["docs"],
    queryFn: async (): Promise<DocSummary[]> => {
      const { docs } = await query(
        "network.habitat.doc.listDocs",
        {},
        { authManager, headers: docsProxyHeaders() },
      );
      return docs;
    },
  });

// Read a doc's canonical CRDT state directly from its space (the member was
// granted read access when the doc was created/updated). The space URI comes
// from the docs list; its second path segment is the owning org DID.
export const docQueryOptions = (docId: string, authManager: AuthManager) =>
  queryOptions({
    queryKey: ["doc", docId],
    queryFn: async ({ client }) => {
      const docs = await client.fetchQuery(docsListQueryOptions(authManager));
      const doc = docs.find((d) => d.docId === docId);
      if (!doc) {
        throw new Error(`doc not found: ${docId}`);
      }
      const orgDid = doc.uri.split("/")[2];
      const record = await query(
        "network.habitat.space.getRecord",
        {
          space: doc.uri,
          repo: orgDid,
          collection: CRDT_COLLECTION,
          rkey: SELF,
        },
        { authManager },
      );
      return record.value as { blob?: string };
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
