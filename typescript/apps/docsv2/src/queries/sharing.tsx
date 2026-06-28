import { queryOptions } from "@tanstack/react-query";
import { AuthManager, procedure, query } from "internal";

// A doc is shared by granting a role on its space. Viewers get read access
// (the "reader" role); editors get read+write access (the "writer" role).
export type ShareRole = "reader" | "writer";

// shareDoc grants the given user a role on a doc's space via a relationship
// tuple. The caller must hold the manager role on the space (the doc creator
// does); pear expands the grant into the FGA graph so listSubjects/listDocs
// pick it up.
export function shareDoc(
  authManager: AuthManager,
  space: string,
  did: string,
  role: ShareRole,
) {
  return procedure(
    "network.habitat.relationship.writeTuple",
    {
      subject: {
        $type: "network.habitat.relationship.defs#userSubject",
        did,
      },
      relation: role,
      object: { space },
    },
    { authManager },
  );
}

export interface DocAccess {
  // DIDs with read-only access.
  viewers: string[];
  // DIDs with read+write access.
  editors: string[];
}

// docAccessQueryOptions lists who currently has access to a doc's space.
// listSubjects(reader) returns everyone who can read — which, by role
// implication, includes writers — so editors are subtracted from the reader set
// to present the two groups distinctly.
export const docAccessQueryOptions = (
  space: string,
  authManager: AuthManager,
) =>
  queryOptions({
    queryKey: ["docAccess", space],
    queryFn: async (): Promise<DocAccess> => {
      const [readers, writers] = await Promise.all([
        query(
          "network.habitat.relationship.listSubjects",
          { space, relation: "reader" },
          { authManager },
        ),
        query(
          "network.habitat.relationship.listSubjects",
          { space, relation: "writer" },
          { authManager },
        ),
      ]);
      const editors = new Set(writers.dids);
      const viewers = readers.dids.filter((did) => !editors.has(did));
      return { viewers, editors: [...editors] };
    },
  });
