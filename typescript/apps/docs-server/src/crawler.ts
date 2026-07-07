import type { DerivedConfig } from "./config";
import type { DocMetadataStore } from "./docMetadataStore";
import type { DocCrdtStore } from "./docCrdtStore";
import type { OrgDirectory } from "./orgDirectory";
import type { DocCommentStore } from "./docCommentStore";
import { DOCS_SPACE_TYPE } from "./pearClient";

// A doc is represented by its rendered-markdown record (which carries the
// title); the crawler keys doc discovery off this collection.
const MARKDOWN_COLLECTION = "network.habitat.docs.markdown";
// The CRDT record holds the doc's Yjs state; the crawler mirrors it into the
// doc CRDT store so edits merge against the latest state without refetching.
const CRDT_COLLECTION = "network.habitat.docs.crdt";
// Space type of an org's self space. Any event on a space of this type means an
// org's membership may have changed, so the org directory is refetched.
const ORG_SPACE_TYPE = "network.habitat.organization";
const RECONNECT_DELAY_MS = 2000;
// Comment records live in a doc's companion comment space, authored by members
// into their own repo; the crawler indexes them under the doc space they name.
const COMMENT_COLLECTION = "network.habitat.docs.comment";

// outboxMessage is sap's wire format for a single outbox event (see
// cmd/sap/websocket.go). The crawler acks it back by id.
interface OutboxMessage {
  id: number;
  uri: string;
  value: unknown;
}

interface ParsedRecordUri {
  spaceUri: string;
  owner: string;
  type: string;
  skey: string;
  repo: string;
  collection: string;
  rkey: string;
}

// parseSpaceRecordUri splits a space-record URI of the form
// ats://<owner>/<type>/<skey>/<repo>/<collection>/<rkey> into the parts the
// crawler needs. Returns undefined if the URI isn't a well-formed record URI.
export function parseSpaceRecordUri(uri: string): ParsedRecordUri | undefined {
  if (!uri.startsWith("ats://")) {
    return undefined;
  }
  const parts = uri.slice("ats://".length).split("/");
  if (parts.length !== 6) {
    return undefined;
  }
  const [owner, type, skey, repo, collection, rkey] = parts;
  if (!owner || !type || !skey || !repo || !collection || !rkey) {
    return undefined;
  }
  return {
    spaceUri: `ats://${owner}/${type}/${skey}`,
    owner,
    type,
    skey,
    repo,
    collection,
    rkey,
  };
}

// Crawler subscribes to sap's outbox channel over the internal websocket, acks
// every message it receives, and persists the docs it discovers (space URI and
// title). Permissions are not indexed; they are resolved on demand at read
// time. It reconnects automatically; unacked messages are redelivered by sap
// on the next connection.
export class Crawler {
  private stopped = false;
  // Serializes message processing so acks are sent in delivery order and the
  // sqlite writes don't interleave.
  private queue: Promise<void> = Promise.resolve();

  constructor(
    private config: DerivedConfig,
    private meta: DocMetadataStore,
    private crdt: DocCrdtStore,
    private orgs: OrgDirectory,
    private comments: DocCommentStore,
  ) {}

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
        console.error("[crawler] connection error", err);
      }
      if (this.stopped) {
        break;
      }
      await delay(RECONNECT_DELAY_MS);
    }
  }

  // connectOnce opens a single websocket and resolves once it closes.
  private connectOnce(): Promise<void> {
    return new Promise<void>((resolve) => {
      const ws = new WebSocket(
        `${this.config.sapUrl.replace(/^http/, "ws")}/channel`,
      );
      ws.addEventListener("open", () => {
        console.log(`[crawler] connected to ${this.config.sapUrl}`);
      });
      ws.addEventListener("message", (ev) => {
        const data = typeof ev.data === "string" ? ev.data : String(ev.data);
        this.enqueue(() => this.handleMessage(ws, data));
      });
      // The close event fires after any error, so it alone resolves the loop.
      ws.addEventListener("close", () => {
        console.log("[crawler] disconnected");
        resolve();
      });
      ws.addEventListener("error", (err) => {
        console.error("[crawler] websocket error", err);
      });
    });
  }

  private enqueue(fn: () => Promise<void>): void {
    this.queue = this.queue.then(fn).catch((err) => {
      console.error("[crawler] handle message", err);
    });
  }

  private async handleMessage(ws: WebSocket, data: string): Promise<void> {
    let msg: OutboxMessage;
    try {
      msg = JSON.parse(data) as OutboxMessage;
    } catch (err) {
      console.error("[crawler] malformed message", data, err);
      return;
    }
    await this.process(msg);
    // Ack every message we receive so sap marks it processed and stops
    // redelivering it. Skip if the socket closed while we were processing;
    // sap will redeliver it on reconnect.
    if (ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ id: msg.id }));
    }
  }

  private async process(msg: OutboxMessage): Promise<void> {
    const parsed = parseSpaceRecordUri(msg.uri);
    if (!parsed) {
      return;
    }
    console.log("[crawler] processing", parsed.collection, "uri:", msg.uri);
    if (parsed.type === ORG_SPACE_TYPE) {
      // The space owner is the org whose membership changed; refetch it.
      await this.orgs.refresh(parsed.owner);
      return;
    }
    if (parsed.collection === MARKDOWN_COLLECTION) {
      const value = (msg.value ?? {}) as { title?: string };
      this.meta.upsertDoc({
        spaceUri: parsed.spaceUri,
        docId: parsed.skey,
        title: value.title || "Untitled",
      });
      return;
    }
    if (parsed.collection === CRDT_COLLECTION) {
      const value = (msg.value ?? {}) as { blob?: string };
      if (value.blob) {
        await this.crdt.upsertState(parsed.spaceUri, value.blob);
      }
      return;
    }
    if (parsed.collection === COMMENT_COLLECTION) {
      const value = (msg.value ?? {}) as {
        body?: string;
        createdAt?: string;
        parent?: string;
        docSpace?: string;
        range?: { start: string; end: string };
      };
      console.log("[crawler] comment received", { body: !!value.body, createdAt: value.createdAt, msgUri: msg.uri });
      if (!value.body || !value.createdAt) {
        console.log("[crawler] comment skipped - missing body or createdAt");
        return;
      }
      // The comment space reuses the doc's skey, so the related doc space is
      // derivable from the comment record's own space URI. We derive it (rather
      // than trusting the record's docSpace field) so a comment can't be
      // attributed to a doc whose comment space the author can't write.
      const docSpace = `ats://${parsed.owner}/${DOCS_SPACE_TYPE}/${parsed.skey}`;
      console.log("[crawler] upserting comment for docSpace:", docSpace);
      this.comments.upsertComment({
        docSpace,
        uri: msg.uri,
        author: parsed.repo,
        body: value.body,
        createdAt: value.createdAt,
        parentUri: value.parent,
        range: value.range,
      });
      console.log("[crawler] comment upserted successfully");
      return;
    }
    // Some other collection; nothing to persist.
  }
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
