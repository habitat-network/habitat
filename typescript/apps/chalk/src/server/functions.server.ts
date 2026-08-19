import fs from "node:fs";
import { DatabaseSync } from "node:sqlite";
import path from "node:path";
import { redirect } from "@tanstack/react-router";
import * as Y from "yjs";
import { renderDoc } from "../render";
import { DebounceQueue } from "./debounceQueue";
import { DocStore } from "./docStore";
import { DocSync } from "./docSync";
import { DocPubSub } from "./pubsub";
import { SapClient } from "./sapClient";
import { useAppSession } from "./session";

// Server-only helpers and the composition root, kept out of functions.ts so
// that file can stay "pure" (only createServerFn-wrapped exports) per
// TanStack Start's server-functions guide
// (https://tanstack.com/start/latest/docs/framework/react/guide/server-functions):
// a file that mixes createServerFn exports with plain functions calling
// server-only APIs (like useAppSession here) can't be statically imported
// from client-reachable code — the whole module, including those plain
// exports' server-only imports, gets pulled into the client bundle graph,
// which the build's import-protection plugin rejects. This file is only
// ever imported by functions.ts (from inside handler bodies, which the
// framework strips from the client bundle regardless of what they import),
// never directly by a route's client component.

export const DOCS_SPACE_TYPE = "network.habitat.docs";
export const CRDT_COLLECTION = "network.habitat.docs.crdt";
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
    throw redirect({ to: "/login" });
  }
  return { did: session.data.did };
}

// memberEditKey/splitMemberEditKey encode the (docId, memberDid) pair as a
// single string DebounceQueue key. docId is a full at:// space URI
// (did:web:... DIDs and all), so a plain delimiter like ":" isn't safe —
// JSON-encoding the pair avoids assuming either half is delimiter-free.
export function memberEditKey(docId: string, memberDid: string): string {
  return JSON.stringify([docId, memberDid]);
}

function splitMemberEditKey(key: string): { docId: string; memberDid: string } {
  const [docId, memberDid] = JSON.parse(key) as [string, string];
  return { docId, memberDid };
}

// --- module-level composition root -----------------------------------
//
// Every piece below is constructed once per process and reused across
// requests: DocStore/DocPubSub/DebounceQueue/DocSync all hold state (sqlite
// connection, in-process subscribers, pending-flush timers, a websocket)
// that only makes sense as a singleton, constructed once at module load.
interface ChalkSingletons {
  store: DocStore;
  pubsub: DocPubSub;
  memberEditQueue: DebounceQueue<string, Uint8Array[]>;
  docSync: DocSync;
}

function createSingletons(): ChalkSingletons {
  const dbPath = process.env.CHALK_DB ?? ".chalk/chalk.db";
  fs.mkdirSync(path.dirname(dbPath), { recursive: true });
  const db = new DatabaseSync(dbPath);
  const store = new DocStore(db);
  const pubsub = new DocPubSub();

  // memberEditQueue debounces each (docId, member) pair's pending Yjs
  // updates before writing them into the member's own repo, per the plan's
  // global debounce policy (2s idle / 10s max). docId is itself the doc's
  // full space URI (see createDoc in functions.ts), so no DocStore lookup
  // is needed to know where to write — this also makes edits work for a
  // doc this chalk instance never created or synced, e.g. one shared with
  // the caller.
  const memberEditQueue = new DebounceQueue<string, Uint8Array[]>({
    idleMs: MEMBER_EDIT_IDLE_MS,
    maxWaitMs: MEMBER_EDIT_MAX_WAIT_MS,
    merge: (existing, incoming) => [...(existing ?? []), ...incoming],
    flush: async (key, updates) => {
      const { docId: spaceUri, memberDid } = splitMemberEditKey(key);
      const client = new SapClient(memberDid);

      // Seeded from the persisted merged state, not a bare `new Y.Doc()`:
      // each queued update is an *incremental* diff (see useYDoc.ts), not a
      // full snapshot, so it's only meaningful relative to the document
      // state it was generated against. Merging incremental diffs alone
      // into an empty doc would upload a blob missing everything that
      // predates this batch — the same bug DocSync.mergeUpdate guards
      // against on the receiving side of this same collection.
      const merged = store.mergedState(spaceUri) ?? new Y.Doc();
      for (const update of updates) {
        Y.applyUpdateV2(merged, update);
      }
      const blob = await client.uploadBlob(
        Y.encodeStateAsUpdateV2(merged),
        "application/octet-stream",
      );
      await client.call("network.habitat.space.putRecord", "POST", {
        space: spaceUri,
        repo: memberDid,
        collection: CRDT_COLLECTION,
        rkey: "self",
        record: { blob: blob.blob },
      });
    },
  });

  const docSync = new DocSync({
    sapWsUrl: (process.env.CHALK_SAP_INTERNAL_URL ?? "").replace(/^http/, "ws"),
    store,
    pubsub,
    render: renderDoc,
  });
  docSync.start();

  return { store, pubsub, memberEditQueue, docSync };
}

declare global {
  // eslint-disable-next-line no-var -- required shape for a globalThis augmentation
  var __chalkSingletons: ChalkSingletons | undefined;
}

// Vite's SSR dev server evaluates this module more than once per process:
// _requireAuth.tsx and $uri.tsx each split into their own server-fn chunk
// that independently imports functions.ts (which imports this file), so
// even a cold boot with zero edits creates two DocSync instances (confirmed
// empirically); Vite's own automatic dependency re-optimization can trigger
// a further reload later, and so can an actual edit. Two live DocSync
// connections to sap's /channel would both select on sap's single-slot
// outbox wakeup signal (see pkg/sap/outbox: "only one consumer should
// drain it at a time"), so whichever loses that race can starve
// indefinitely. Pinning the singletons on globalThis instead of a bare
// module-level const collapses every such re-evaluation down to one
// instance.
export const { store, pubsub, memberEditQueue, docSync } =
  (globalThis.__chalkSingletons ??= createSingletons());

// globalThis persists across a real HMR replacement too, though, so
// without disposal an actual edit to this file (or docSync.ts,
// sapClient.ts, etc.) would silently keep running the *old* code forever.
// dispose() runs right before Vite swaps this module instance out — tear
// down the old DocSync (closing its live connection) and clear the pinned
// value, so the next evaluation's ??= builds a fresh one from the new code
// instead of finding the stale one still set.
if (import.meta.hot) {
  import.meta.hot.dispose(() => {
    globalThis.__chalkSingletons?.docSync.stop();
    globalThis.__chalkSingletons = undefined;
  });
}
