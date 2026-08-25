import { createServerFn } from "@tanstack/react-start";
import { env } from "cloudflare:workers";
import { docsForOwner, getDb, upsertDoc, type DocSummary } from "../db";
import { readFrames } from "./rooms/frames";
import {
  DOCS_SPACE_TYPE,
  clearSession,
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
    return docsForOwner(getDb(env), did);
  },
);

export const sendEdit = createServerFn({ method: "POST" })
  .validator((input: { docId: string; update: Uint8Array }) => input)
  .handler(async ({ data }) => {
    const { did } = await requireSession();
    // One RPC, not two: the immediate fanout and the debounced repo
    // writeback both live in the same object now.
    await env.DOC.get(env.DOC.idFromName(data.docId)).applyEdit(
      { spaceUri: data.docId },
      did,
      data.update,
    );
  });

// subscribeDoc streams a doc's merged state: its current snapshot first (if
// one exists yet), then every subsequent merge DocRoom broadcasts. docId is
// itself the doc's full space URI, so this works whether the caller is the
// doc's owner or another editor it's been shared with, and even for a doc
// this deployment's D1 index has never seen — unlike deriving the space URI
// from that index or from the caller's own DID.
//
// Yields the raw Yjs update bytes directly rather than base64 — seroval (the
// serializer behind TanStack Start's server-fn RPC, including this streamed
// path) has native Uint8Array support, so encoding to a string first is
// unnecessary overhead.
export const subscribeDoc = createServerFn({ method: "GET" })
  .validator((input: { docId: string }) => input)
  .handler(async function* ({ data }): AsyncGenerator<{ state: Uint8Array }> {
    const { did } = await requireSession();
    await requireDocAccess(new SapClient(env, did), did, data.docId);

    const stub = env.DOC.get(env.DOC.idFromName(data.docId));
    const stream = await stub.subscribe();
    for await (const state of readFrames(stream)) {
      yield { state };
    }
  });

// A userRelation record as network.habitat.relationship.listRelations
// returns it — only the fields sharing.ts actually reads.
interface UserRelationView {
  uri: string;
  subject: string;
  relation: string;
}

// requireDocAccess is the one place chalk actually checks
// network.habitat.relationship before returning a doc's content: DocRoom
// itself has no ACL, so it would otherwise fan out to whoever asks. A
// caller who doesn't already hold at least reader has that same check
// endpoint reject the query itself (it requires reader to call), not
// return { allowed: false } — both outcomes mean "no access" here.
async function requireDocAccess(
  client: SapClient,
  did: string,
  docId: string,
): Promise<void> {
  let allowed = false;
  try {
    const res = await client.call<{ allowed: boolean }>(
      "network.habitat.relationship.checkUserRelation",
      "GET",
      { subject: did, relation: "reader", space: docId },
    );
    allowed = res.allowed;
  } catch {
    allowed = false;
  }
  if (!allowed) throw new Error("You do not have access to this doc");
}

// listDocAccess returns the DIDs of every user with a direct grant on the
// doc — the "people with access" list a share dialog shows. Only user
// grants (subjectType "user"), not space/group usersets: chalk's sharing
// is user-to-user for now.
export const listDocAccess = createServerFn({ method: "GET" })
  .validator((input: { docId: string }) => input)
  .handler(async ({ data }): Promise<{ did: string }[]> => {
    const { did } = await requireSession();
    const client = new SapClient(env, did);
    const { relations } = await client.call<{ relations: UserRelationView[] }>(
      "network.habitat.relationship.listRelations",
      "GET",
      { space: data.docId, subjectType: "user" },
    );
    return relations.map((r) => ({ did: r.subject }));
  });

// shareDoc grants a user write access to a doc. Sharing a doc means being
// able to edit it — chalk has no read-only collaborator concept — so this
// always grants "writer", not the "reader" a bare share link might imply
// elsewhere. Requires the caller to already hold manager (pear enforces
// this; a non-manager's setUserRelation call fails there, not here).
export const shareDoc = createServerFn({ method: "POST" })
  .validator((input: { docId: string; subjectDid: string }) => input)
  .handler(async ({ data }) => {
    const { did } = await requireSession();
    const client = new SapClient(env, did);
    await client.call("network.habitat.relationship.setUserRelation", "POST", {
      subject: data.subjectDid,
      relation: "writer",
      space: data.docId,
    });
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
