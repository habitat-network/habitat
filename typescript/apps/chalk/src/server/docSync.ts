import * as Y from "yjs";
import { DebounceQueue } from "./debounceQueue";
import type { DocStore } from "./docStore";
import type { DocPubSub } from "./pubsub";
import { SapClient } from "./sapClient";

// CRDT_COLLECTION is the collection a member writes their per-repo Yjs delta
// to; any message on this collection triggers a merge into the space's Y.Doc.
// MARKDOWN_COLLECTION carries the rendered title/content, written back to the
// owner's repo alongside the CRDT snapshot on republish.
const CRDT_COLLECTION = "network.habitat.docs.crdt";
const MARKDOWN_COLLECTION = "network.habitat.docs.markdown";
const SELF = "self";
const RECONNECT_DELAY_MS = 2000;
// Global debounce policy for the owner-snapshot republish: flush after 2s of
// no further merges for a space, or after 10s regardless, whichever first.
const OWNER_FLUSH_IDLE_MS = 2000;
const OWNER_FLUSH_MAX_WAIT_MS = 10000;

// OutboxMessage is sap's wire format for a single outbox event delivered over
// the /channel websocket (see cmd/sap/websocket.go outboxWireMessage). The
// consumer acks it back by id ({id}).
export interface OutboxMessage {
  id: number;
  uri: string;
  value: unknown;
}

export interface ParsedSpaceRecordUri {
  spaceUri: string;
  owner: string;
  type: string;
  skey: string;
  repo: string;
  collection: string;
}

// parseSpaceRecordUri splits a space-record URI
// (at://<owner>/space/<type>/<skey>/<repo>/<collection>/<rkey>, per
// internal/syntax.ConstructSpaceRecordURI) into its parts. Returns undefined
// if the URI isn't well-formed.
export function parseSpaceRecordUri(
  uri: string,
): ParsedSpaceRecordUri | undefined {
  if (!uri.startsWith("at://")) return undefined;
  const parts = uri.slice("at://".length).split("/");
  if (parts.length !== 7 || parts[1] !== "space") return undefined;
  const [owner, , type, skey, repo, collection] = parts;
  if (!owner || !type || !skey || !repo || !collection) return undefined;
  return {
    spaceUri: `at://${owner}/space/${type}/${skey}`,
    owner,
    type,
    skey,
    repo,
    collection,
  };
}

export interface DocSyncOptions {
  sapWsUrl: string;
  store: DocStore;
  pubsub: DocPubSub;
  render: (ydoc: Y.Doc) => { title: string; markdown: string };
}

// DocSync subscribes to sap's outbox channel and, for every member's
// per-repo crdt record it sees, merges the delta into that space's
// in-memory Y.Doc, persists and publishes the merged result, and
// debounce-schedules a republish of the canonical merged markdown+CRDT
// snapshot back to pear under the doc owner's own repo. Unlike docs-server's
// Crawler (one org-owned record per doc), any member's repo can contribute a
// delta, and there is no org-space branch at all.
export class DocSync {
  private stopped = false;
  // Tracked so stop() can close the live connection immediately, rather
  // than just suppressing the next reconnect — otherwise a stopped
  // instance would keep an open /channel connection (and keep competing
  // for sap's single-slot outbox wakeup signal, see pkg/sap/outbox) for as
  // long as sap happens to leave it open.
  private ws: WebSocket | undefined;
  // Serializes message processing so acks are sent in delivery order and
  // merges for the same space don't interleave.
  private queue: Promise<void> = Promise.resolve();
  private ydocs = new Map<string, Y.Doc>();
  private ownerFlush: DebounceQueue<string, void>;

  constructor(private opts: DocSyncOptions) {
    this.ownerFlush = new DebounceQueue<string, void>({
      idleMs: OWNER_FLUSH_IDLE_MS,
      maxWaitMs: OWNER_FLUSH_MAX_WAIT_MS,
      merge: () => undefined,
      flush: (spaceUri) => this.republishCanonical(spaceUri),
    });
  }

  // start runs the connect/reconnect loop in the background.
  start(): void {
    void this.run();
  }

  stop(): void {
    this.stopped = true;
    this.ws?.close();
  }

  private async run(): Promise<void> {
    while (!this.stopped) {
      try {
        await this.connectOnce();
      } catch (err) {
        console.error("[docSync] connection error", err);
      }
      if (this.stopped) break;
      await new Promise((resolve) => setTimeout(resolve, RECONNECT_DELAY_MS));
    }
  }

  // connectOnce opens a single websocket and resolves once it closes.
  private connectOnce(): Promise<void> {
    return new Promise<void>((resolve) => {
      const ws = new WebSocket(`${this.opts.sapWsUrl}/channel`);
      this.ws = ws;
      ws.addEventListener("open", () => {
        console.log(`[docSync] connected to ${this.opts.sapWsUrl}`);
      });
      ws.addEventListener("message", (ev) => {
        const data = typeof ev.data === "string" ? ev.data : String(ev.data);
        this.enqueue(() => this.handleRawMessage(ws, data));
      });
      // The close event fires after any error, so it alone resolves the loop.
      ws.addEventListener("close", () => {
        if (this.ws === ws) this.ws = undefined;
        resolve();
      });
    });
  }

  private enqueue(fn: () => Promise<void>): void {
    this.queue = this.queue.then(fn).catch((err) => {
      console.error("[docSync] handle message", err);
    });
  }

  private async handleRawMessage(ws: WebSocket, data: string): Promise<void> {
    let msg: OutboxMessage;
    try {
      msg = JSON.parse(data) as OutboxMessage;
    } catch (err) {
      console.error("[docSync] malformed message", data, err);
      return;
    }
    await this.handleOutboxMessage(msg);
    // Ack every message we receive so sap marks it processed and stops
    // redelivering it. Skip if the socket closed while we were processing;
    // sap will redeliver it on reconnect.
    if (ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ id: msg.id }));
    }
  }

  // handleOutboxMessage merges a single sap-outbox event. Exposed as its own
  // method (rather than folded into the websocket handler) so it's testable
  // without a live connection.
  async handleOutboxMessage(msg: OutboxMessage): Promise<void> {
    const parsed = parseSpaceRecordUri(msg.uri);
    if (!parsed || parsed.collection !== CRDT_COLLECTION) return;

    // A putRecord'd network.habitat.docs.crdt record carries a blob
    // *reference* ({$type: "blob", ref: {$link: <cid>}, ...}), not the
    // update's bytes inline — those have to be fetched separately via
    // getBlob.
    const value = (msg.value ?? {}) as { blob?: { ref?: { $link?: string } } };
    const cid = value.blob?.ref?.$link;
    if (!cid) return;

    const [doc] = this.opts.store.docsByUris([parsed.spaceUri]);
    if (!doc) return; // this instance doesn't know this doc; nothing to merge into
    const bytes = await new SapClient(doc.ownerDid).getBlob(
      parsed.spaceUri,
      cid,
    );
    this.mergeUpdate(parsed.spaceUri, bytes);
  }

  // applyEdit merges a Yjs update straight from a connected editor (see
  // sendEdit in functions.ts) into the space's in-memory Y.Doc immediately,
  // rather than waiting on the full pear -> sap notifyWrite -> outbox round
  // trip that handleOutboxMessage above reacts to. That round trip still
  // runs (sendEdit also queues the same bytes for the member's own repo via
  // memberEditQueue) and will eventually deliver the same update back here
  // through handleOutboxMessage — but by then it's a redundant merge: Y.Doc
  // updates are idempotent, so re-applying bytes already merged is a no-op
  // rather than a correctness concern. This is what lets the editor's own
  // session, and any other subscriber of the same doc on this instance, see
  // an edit right away instead of only after that round trip completes.
  applyEdit(spaceUri: string, bytes: Uint8Array): void {
    this.mergeUpdate(spaceUri, bytes);
  }

  // mergeUpdate applies a Yjs update to spaceUri's in-memory Y.Doc, persists
  // and publishes the result, and debounce-schedules the owner-canonical
  // republish — the common tail shared by a remote merge
  // (handleOutboxMessage) and a local one (applyEdit).
  private mergeUpdate(spaceUri: string, bytes: Uint8Array): void {
    let ydoc = this.ydocs.get(spaceUri);
    if (!ydoc) {
      // Not just `new Y.Doc()`: the in-memory map starts empty on every
      // process start, but bytes here is an *incremental* Yjs update (a
      // diff relative to whatever state the sender already had), not a full
      // snapshot — merging it into a bare empty doc would reconstruct only
      // that one diff, and persistMerged below would then overwrite this
      // space's previously-persisted content with that fragment. Seeding
      // from the persisted merged state (falling back to empty only for a
      // genuinely new, never-persisted doc) keeps this doc's history intact
      // across restarts.
      ydoc = this.opts.store.mergedState(spaceUri) ?? new Y.Doc();
      this.ydocs.set(spaceUri, ydoc);
    }
    Y.applyUpdateV2(ydoc, bytes);

    this.opts.store.persistMerged(spaceUri, ydoc);
    this.opts.pubsub.publish(spaceUri, ydoc);
    this.ownerFlush.push(spaceUri, undefined);
  }

  // republishCanonical writes the merged markdown + CRDT snapshot back to
  // pear under the doc owner's own repo, authenticated as the owner.
  // Republishing writes to the owner's own crdt record, which is itself a
  // network.habitat.docs.crdt write — so it comes right back through
  // handleOutboxMessage looking like a fresh delta. That doesn't loop
  // forever, though: pear's PutRecord skips advancing the record (and the
  // notifyWrite it would otherwise cause) whenever a write doesn't actually
  // change the record's CID (see internal/spaces/store.go), so a no-op
  // republish here never produces another outbox event to react to.
  private async republishCanonical(spaceUri: string): Promise<void> {
    const ydoc = this.ydocs.get(spaceUri);
    if (!ydoc) return;

    const [doc] = this.opts.store.docsByUris([spaceUri]);
    if (!doc) return;
    const rendered = this.opts.render(ydoc);
    const client = new SapClient(doc.ownerDid);
    const blob = await client.uploadBlob(
      Y.encodeStateAsUpdateV2(ydoc),
      "application/octet-stream",
    );
    await client.call("network.habitat.space.putRecord", "POST", {
      space: spaceUri,
      repo: doc.ownerDid,
      collection: CRDT_COLLECTION,
      rkey: SELF,
      record: { blob: blob.blob },
    });
    await client.call("network.habitat.space.putRecord", "POST", {
      space: spaceUri,
      repo: doc.ownerDid,
      collection: MARKDOWN_COLLECTION,
      rkey: SELF,
      record: { title: rendered.title, content: rendered.markdown },
    });
    this.opts.store.upsertDoc({
      spaceUri,
      docId: doc.docId,
      ownerDid: doc.ownerDid,
      title: rendered.title,
    });
  }
}
