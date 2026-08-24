import { createServerFn } from "@tanstack/react-start";
import { env } from "cloudflare:workers";
import * as Y from "yjs";
import { docsForOwner, getDb, upsertDoc, type DocSummary } from "../db";
import {
  DOCS_SPACE_TYPE,
  docSync,
  memberEditKey,
  memberEditQueue,
  pubsub,
  clearSession,
  requireSession,
  store,
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

// Task 1 spike: proves a server function can reach a Durable Object binding
// via `env` from `cloudflare:workers` — Cloudflare's canonical way to read
// bindings, including from module scope where `process.env` does not exist
// on workerd. Task 9 removes this once the real rooms exist.
export const ping = createServerFn({ method: "GET" }).handler(async () => {
  const stub = env.PING.get(env.PING.idFromName("a"));
  return stub.bump();
});

export const createDoc = createServerFn({ method: "POST" }).handler(
  async (): Promise<{ docId: string; uri: string }> => {
    const { did } = await requireSession();
    const client = new SapClient(did);

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
    // Merge into this instance's in-memory doc (and publish to subscribers)
    // right away, rather than waiting on the queued repo write below to
    // round-trip through pear -> sap's outbox back to handleOutboxMessage —
    // see applyEdit's comment for why that round trip is safe to leave in
    // place as a redundant, idempotent confirmation rather than removing it.
    docSync.applyEdit(data.docId, data.update);
    memberEditQueue.push(memberEditKey(data.docId, did), [data.update]);
  });

// subscribeDoc streams a doc's merged state: its current snapshot first (if
// one exists yet), then every subsequent merge DocSync publishes. docId is
// itself the doc's full space URI, so this works whether the caller is the
// doc's owner or (once sharing exists) another editor, and even for a doc
// this chalk instance's DocStore has never seen — unlike deriving the space
// URI from a DocStore lookup or from the caller's own DID.
//
// Yields the raw Yjs update bytes directly rather than base64 — seroval (the
// serializer behind TanStack Start's server-fn RPC, including this streamed
// path) has native Uint8Array support, so encoding to a string first is
// unnecessary overhead.
export const subscribeDoc = createServerFn({ method: "GET" })
  .validator((input: { docId: string }) => input)
  .handler(async function* ({ data }): AsyncGenerator<{ state: Uint8Array }> {
    await requireSession();

    const spaceUri = data.docId;
    const current = store.mergedState(spaceUri);
    if (current) {
      yield { state: Y.encodeStateAsUpdateV2(current) };
    }
    for await (const ydoc of pubsub.subscribe(spaceUri)) {
      yield { state: Y.encodeStateAsUpdateV2(ydoc) };
    }
  });
