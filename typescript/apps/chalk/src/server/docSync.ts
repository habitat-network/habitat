import * as Y from "yjs";
import { DebounceQueue } from "./debounceQueue";
import type { DocStore } from "./docStore";
import type { DocPubSub } from "./pubsub";
import type { SapClient } from "./sapClient";

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
  // ownerClientFor returns a SapClient authenticated as the doc's owner, used
  // to republish the canonical merged snapshot under the owner's own repo.
  ownerClientFor: (ownerDid: string) => SapClient;
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
      ws.addEventListener("open", () => {
        console.log(`[docSync] connected to ${this.opts.sapWsUrl}`);
      });
      ws.addEventListener("message", (ev) => {
        const data = typeof ev.data === "string" ? ev.data : String(ev.data);
        this.enqueue(() => this.handleRawMessage(ws, data));
      });
      // The close event fires after any error, so it alone resolves the loop.
      ws.addEventListener("close", () => resolve());
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
    const value = (msg.value ?? {}) as { blob?: string };
    if (!value.blob) return;

    const ydoc = this.ydocs.get(parsed.spaceUri) ?? new Y.Doc();
    Y.applyUpdateV2(ydoc, Buffer.from(value.blob, "base64"));
    this.ydocs.set(parsed.spaceUri, ydoc);

    this.opts.store.persistMerged(parsed.spaceUri, ydoc);
    this.opts.pubsub.publish(parsed.spaceUri, ydoc);
    this.ownerFlush.push(parsed.spaceUri, undefined);
  }

  // republishCanonical writes the merged markdown + CRDT snapshot back to
  // pear under the doc owner's own repo, authenticated as the owner.
  private async republishCanonical(spaceUri: string): Promise<void> {
    const ydoc = this.ydocs.get(spaceUri);
    if (!ydoc) return;
    const [doc] = this.opts.store.docsByUris([spaceUri]);
    if (!doc) return;
    const rendered = this.opts.render(ydoc);
    const client = this.opts.ownerClientFor(doc.ownerDid);
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
