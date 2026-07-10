import { queryOptions } from "@tanstack/react-query";
import { procedure, query } from "internal";
import type { DocsServerFetcher } from "@/docsServerFetcher";

const CRDT_COLLECTION = "network.habitat.docs.crdt";
const SELF = "self";

export interface DocSummary {
  docId: string;
  uri: string;
  title: string;
}

// The sidebar list comes entirely from the docs server's listDocs endpoint,
// which enumerates the org's doc spaces and reads each one's markdown title. The
// fetcher points straight at the docs server; its server session authenticates
// the request.
export const docsListQueryOptions = (fetcher: DocsServerFetcher) =>
  queryOptions({
    queryKey: ["docs"],
    queryFn: async (): Promise<DocSummary[]> => {
      const { docs } = await query(
        "network.habitat.docs.listDocs",
        {},
        { fetcher },
      );
      return docs;
    },
  });

// Read a doc's merged CRDT state from the docs server. The space no longer holds
// a single "self" CRDT record — each editor writes their own — so the docs
// server serves getRecord for the CRDT collection from its merged state. The
// space URI comes from the docs list; its second path segment is the owning org
// DID.
export const docQueryOptions = (docId: string, fetcher: DocsServerFetcher) =>
  queryOptions({
    queryKey: ["doc", docId],
    queryFn: async ({ client }) => {
      const docs = await client.fetchQuery(docsListQueryOptions(fetcher));
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
        { fetcher },
      );
      return record.value as { blob?: string };
    },
  });

export function createDoc(fetcher: DocsServerFetcher) {
  return procedure("network.habitat.docs.createDoc", {}, { fetcher });
}

// pushUpdate sends a base64-encoded Yjs update to the docs server, which merges
// it into the doc and writes it back as the user's own CRDT record.
export function pushUpdate(
  fetcher: DocsServerFetcher,
  docId: string,
  update: string,
) {
  return procedure(
    "network.habitat.docs.updateDoc",
    { docId, update },
    { fetcher },
  );
}
