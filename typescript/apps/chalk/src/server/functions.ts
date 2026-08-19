import fs from "node:fs";
import { DatabaseSync } from "node:sqlite";
import path from "node:path";
import { redirect } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import * as Y from "yjs";
import { renderDoc } from "../render";
import { DebounceQueue } from "./debounceQueue";
import { DocStore, type DocSummary } from "./docStore";
import { DocSync } from "./docSync";
import { DocPubSub } from "./pubsub";
import { SapClient } from "./sapClient";
import { useAppSession } from "./session";

const DOCS_SPACE_TYPE = "network.habitat.docs";
const CRDT_COLLECTION = "network.habitat.docs.crdt";
// Debounce policy shared by both write paths in chalk (see plan's Global
// Constraints: "flush a member's pending edits 2s after the last edit if
// idle, but never let more than 10s of edits go unpersisted"). DocSync
// applies the same values to its owner-snapshot republish.
const MEMBER_EDIT_IDLE_MS = 2000;
const MEMBER_EDIT_MAX_WAIT_MS = 10000;

// requireSession resolves the logged-in member's DID from the session
// cookie, throwing a redirect to /login when there isn't one.
export async function requireSession(): Promise<{ did: string }> {
  const session = await useAppSession();
  if (!session.data.did) {
    // "/login" isn't a registered route yet — the login route itself is
    // Task 10's job, out of this task's scope — so the router's generated
    // route-union type doesn't include it until then. Cast rather than
    // block this task on a route this task doesn't own.
    throw redirect({ to: "/login" as "." });
  }
  return { did: session.data.did };
}

function sapInternalUrl(): string {
  const url = process.env.CHALK_SAP_INTERNAL_URL;
  if (!url) throw new Error("CHALK_SAP_INTERNAL_URL is not set");
  return url;
}

// --- module-level composition root -----------------------------------
//
// Every piece below is constructed once per process and reused across
// requests: DocStore/DocPubSub/DebounceQueue/DocSync all hold state (sqlite
// connection, in-process subscribers, pending-flush timers, a websocket)
// that only makes sense as a singleton.

const dbPath = process.env.CHALK_DB ?? ".chalk/chalk.db";
fs.mkdirSync(path.dirname(dbPath), { recursive: true });
const db = new DatabaseSync(dbPath);
const store = new DocStore(db);
const pubsub = new DocPubSub();

// sessionIdCache maps a member DID to the sap session ID sap resumes it
// with, populated from sap's GET /org/list (see cmd/sap/server.go's
// handleListOrgs, which now returns {did, sessionId} pairs instead of bare
// DIDs). Populated lazily by requireSessionIdFor and read synchronously by
// ownerClientFor below (DocSync's republish hook has no way to await a
// lookup), so a DID must have gone through requireSessionIdFor at least
// once — which every createDoc/sendEdit call does for its own caller —
// before DocSync can republish snapshots owned by it.
const sessionIdCache = new Map<string, string>();

// requireSessionIdFor resolves the sap session ID tracked for did, via
// sap's /org/list. Refreshes the whole cache from sap on a miss (cheap:
// sap tracks one session per DID, so the list is small) rather than
// requiring a dedicated per-DID sap endpoint.
async function requireSessionIdFor(did: string): Promise<string> {
  const cached = sessionIdCache.get(did);
  if (cached) return cached;

  const res = await fetch(`${sapInternalUrl()}/org/list`);
  if (!res.ok) {
    throw new Error(`failed to list sap sessions (${res.status})`);
  }
  const { orgs } = (await res.json()) as {
    orgs: { did: string; sessionId: string }[];
  };
  for (const org of orgs) {
    sessionIdCache.set(org.did, org.sessionId);
  }

  const sessionId = sessionIdCache.get(did);
  if (!sessionId) {
    throw new Error(`no sap session on file for ${did}`);
  }
  return sessionId;
}

// ownerClientFor returns a SapClient authenticated as a doc's owner, for
// DocSync's canonical-snapshot republish. Synchronous (DocSync's hook can't
// await), so it only ever reads sessionIdCache — which createDoc primes for
// every doc's owner at creation time.
function ownerClientFor(ownerDid: string): SapClient {
  const sessionId = sessionIdCache.get(ownerDid);
  if (!sessionId) {
    throw new Error(
      `no cached sap session for owner ${ownerDid}; they must call a chalk ` +
        `server function (e.g. createDoc) at least once before a republish can happen`,
    );
  }
  return new SapClient(ownerDid, sessionId);
}

// memberEditQueue debounces each (docId, member) pair's pending Yjs updates
// before writing them into the member's own repo, per the plan's global
// debounce policy (2s idle / 10s max).
const memberEditQueue = new DebounceQueue<string, Uint8Array[]>({
  idleMs: MEMBER_EDIT_IDLE_MS,
  maxWaitMs: MEMBER_EDIT_MAX_WAIT_MS,
  merge: (existing, incoming) => [...(existing ?? []), ...incoming],
  flush: async (key, updates) => {
    const { docId, memberDid } = splitMemberEditKey(key);
    const doc = store.docByDocId(docId);
    if (!doc) {
      // The doc was created after this flush was scheduled and somehow
      // vanished, or docId was bogus; nothing sane to write to.
      console.error(`[functions] sendEdit flush: unknown docId ${docId}`);
      return;
    }

    const sessionId = await requireSessionIdFor(memberDid);
    const client = new SapClient(memberDid, sessionId);

    const merged = new Y.Doc();
    for (const update of updates) {
      Y.applyUpdateV2(merged, update);
    }
    const blob = await client.uploadBlob(
      Y.encodeStateAsUpdateV2(merged),
      "application/octet-stream",
    );
    await client.call("network.habitat.space.putRecord", "POST", {
      space: doc.uri,
      repo: memberDid,
      collection: CRDT_COLLECTION,
      rkey: "self",
      record: { blob: blob.blob },
    });
  },
});

// memberEditKey/splitMemberEditKey encode the (docId, memberDid) pair as a
// single string DebounceQueue key. DIDs (did:plc:...) contain colons, so we
// split only on the first colon rather than assuming a delimiter absent
// from both halves; docId (a TID-derived skey) never contains one.
function memberEditKey(docId: string, memberDid: string): string {
  return `${docId}:${memberDid}`;
}

function splitMemberEditKey(key: string): { docId: string; memberDid: string } {
  const sep = key.indexOf(":");
  return { docId: key.slice(0, sep), memberDid: key.slice(sep + 1) };
}

const docSync = new DocSync({
  sapWsUrl: (process.env.CHALK_SAP_INTERNAL_URL ?? "").replace(/^http/, "ws"),
  store,
  pubsub,
  ownerClientFor,
  render: renderDoc,
});
docSync.start();

// --- server functions ---------------------------------------------------

export const createDoc = createServerFn({ method: "POST" }).handler(
  async (): Promise<{ docId: string; uri: string }> => {
    const { did } = await requireSession();
    const sessionId = await requireSessionIdFor(did);
    const client = new SapClient(did, sessionId);

    const created = await client.call<{ uri: string }>(
      "network.habitat.simplespace.createSpace",
      "POST",
      { did, type: DOCS_SPACE_TYPE },
    );
    const docId = created.uri.split("/").pop();
    if (!docId) {
      throw new Error(`createSpace returned an unparsable uri: ${created.uri}`);
    }

    store.upsertDoc({
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
    return store.docsForOwner(did);
  },
);

export const sendEdit = createServerFn({ method: "POST" })
  .validator((input: { docId: string; update: string }) => input)
  .handler(async ({ data }) => {
    const { did } = await requireSession();
    memberEditQueue.push(memberEditKey(data.docId, did), [
      Buffer.from(data.update, "base64"),
    ]);
  });

// subscribeDoc streams a doc's merged state: its current snapshot first (if
// one exists yet), then every subsequent merge DocSync publishes. Resolves
// the space via DocStore's docId -> DocSummary index, so it works whether
// the caller is the doc's owner or (once sharing exists) another editor —
// unlike deriving the space URI from the caller's own DID, which only
// happens to work for the owner.
export async function* subscribeDoc(input: {
  docId: string;
}): AsyncGenerator<{ state: string }> {
  await requireSession();

  const doc = store.docByDocId(input.docId);
  if (!doc) {
    throw new Error(`unknown doc ${input.docId}`);
  }

  const current = store.mergedState(doc.uri);
  if (current) {
    yield {
      state: Buffer.from(Y.encodeStateAsUpdateV2(current)).toString("base64"),
    };
  }
  for await (const ydoc of pubsub.subscribe(doc.uri)) {
    yield {
      state: Buffer.from(Y.encodeStateAsUpdateV2(ydoc)).toString("base64"),
    };
  }
}
