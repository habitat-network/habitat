import { createServerFn } from "@tanstack/react-start";
import { env } from "cloudflare:workers";
import { docsForAccessor, getDb, upsertDoc, type DocSummary } from "../db";
import {
  DOCS_SPACE_TYPE,
  clearSession,
  docRole,
  requireSession,
} from "./functions.server";
import { SapClient } from "./sapClient";

// Every export below is a createServerFn wrapper — safe to statically
// import from any client-reachable file (route components included), per
// TanStack Start's server-functions guide: the build replaces each of
// these with an RPC stub in the client bundle, and the actual handler body
// (plus whatever it imports, e.g. functions.server.ts's server-only code)
// never reaches the browser.

// getCaller resolves the logged-in member's DID, redirecting to /login when
// there isn't one — the one auth check every route/mutation below shares.
export const getCaller = createServerFn({ method: "GET" }).handler(async () =>
  requireSession(),
);

// signOut clears the session cookie. The session cookie is httpOnly, so
// the client can't drop it itself — it has to go through the server.
export const signOut = createServerFn({ method: "POST" }).handler(async () => {
  await clearSession();
});

export const createDoc = createServerFn({ method: "POST" }).handler(
  async (): Promise<{ docId: string; uri: string }> => {
    const { did } = await requireSession();
    const client = new SapClient(env, did);

    const created = await client.call<{ uri: string }>(
      "network.habitat.simplespace.createSpace",
      "POST",
      { did, type: DOCS_SPACE_TYPE },
    );

    // sap has no way to discover this space on its own until the member's
    // next session crawl — tell it explicitly so DocSync's outbox consumer
    // actually receives events for edits to it.
    await client.trackSpace(created.uri);

    // docId is the doc's full space URI, not just its trailing skey: a
    // space's skey alone doesn't say which host/repo it lives under, so a
    // bare skey can't be resolved back to a space without already having it
    // in this instance's DocStore — which breaks for a doc shared with a
    // member whose chalk instance never created or synced it. The full URI
    // is self-describing.
    const docId = created.uri;

    await upsertDoc(getDb(env), {
      spaceUri: created.uri,
      docId,
      ownerDid: did,
      title: "Untitled",
    });

    // Record the room's identity now, so the owner-republish alarm knows the
    // owner before the webhook (src/server/webhook.ts) delivers it.
    await env.DOC.get(env.DOC.idFromName(created.uri)).seedIdentity({
      spaceUri: created.uri,
      ownerDid: did,
    });

    return { docId, uri: created.uri };
  },
);

export const listDocs = createServerFn({ method: "GET" }).handler(
  async (): Promise<DocSummary[]> => {
    const { did } = await requireSession();
    return docsForAccessor(getDb(env), did);
  },
);

// A userRelation record as network.habitat.relationship.listRelations
// returns it — only the fields sharing.ts actually reads.
interface UserRelationView {
  uri: string;
  subject: string;
  relation: string;
}

// listDocAccess returns every user with a direct grant on the doc, and
// their relation — the "people with access" list a share dialog shows.
// Only user grants (subjectType "user"), not space/group usersets:
// chalk's sharing is user-to-user for now.
export const listDocAccess = createServerFn({ method: "GET" })
  .validator((input: { docId: string }) => input)
  .handler(
    async ({
      data,
    }): Promise<{ did: string; relation: "writer" | "reader" }[]> => {
      const { did } = await requireSession();
      const client = new SapClient(env, did);
      const { relations } = await client.call<{
        relations: UserRelationView[];
      }>("network.habitat.relationship.listRelations", "GET", {
        space: data.docId,
        subjectType: "user",
      });
      return relations.map((r) => ({
        did: r.subject,
        relation: r.relation as "writer" | "reader",
      }));
    },
  );

// A doc grantee is either an editor (can edit the doc) or a viewer
// (read-only), which map to the "writer"/"reader" relations
// network.habitat.relationship actually stores.
const ROLE_TO_RELATION: Record<"editor" | "viewer", "writer" | "reader"> = {
  editor: "writer",
  viewer: "reader",
};

// shareDoc grants a user access to a doc as either an editor or a viewer.
// Requires the caller to already hold manager (pear enforces this; a
// non-manager's setUserRelation call fails there, not here).
export const shareDoc = createServerFn({ method: "POST" })
  .validator(
    (input: { docId: string; subjectDid: string; role: "editor" | "viewer" }) =>
      input,
  )
  .handler(async ({ data }) => {
    const { did } = await requireSession();
    const client = new SapClient(env, did);
    await client.call("network.habitat.relationship.setUserRelation", "POST", {
      subject: data.subjectDid,
      relation: ROLE_TO_RELATION[data.role],
      space: data.docId,
    });
  });

// getDocRole resolves the caller's own role on the doc, so the client can
// keep the editor read-only for a viewer. See docRole's comment: this is
// a UX signal, not the access gate itself.
export const getDocRole = createServerFn({ method: "GET" })
  .validator((input: { docId: string }) => input)
  .handler(async ({ data }): Promise<"editor" | "viewer" | null> => {
    const { did } = await requireSession();
    const client = new SapClient(env, did);
    return docRole(client, did, data.docId);
  });

// revokeDocAccess removes a user's grant. deleteRelation takes the relation
// record's own URI, not a (did, space) pair, so this looks that URI up via
// the same listRelations query listDocAccess uses, filtered to the one
// subject — no separate index of grant URIs needs to be kept anywhere.
export const revokeDocAccess = createServerFn({ method: "POST" })
  .validator((input: { docId: string; subjectDid: string }) => input)
  .handler(async ({ data }) => {
    const { did } = await requireSession();
    const client = new SapClient(env, did);
    const { relations } = await client.call<{ relations: UserRelationView[] }>(
      "network.habitat.relationship.listRelations",
      "GET",
      { space: data.docId, subjectType: "user", subjectDid: data.subjectDid },
    );
    const relation = relations[0];
    if (!relation) return;
    await client.call("network.habitat.relationship.deleteRelation", "POST", {
      uri: relation.uri,
    });
  });
