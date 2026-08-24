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
    // owner before any SapChannel delivery supplies it.
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
// doc's owner or (once sharing exists) another editor, and even for a doc
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
    await requireSession();

    const stub = env.DOC.get(env.DOC.idFromName(data.docId));
    const res = await stub.fetch("https://do/subscribe");
    for await (const state of readFrames(res.body!)) {
      yield { state };
    }
  });
