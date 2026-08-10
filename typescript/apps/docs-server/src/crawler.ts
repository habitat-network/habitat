import type { DerivedConfig } from "./config";
import type { DocMetadataStore } from "./docMetadataStore";
import type { DocCrdtStore } from "./docCrdtStore";
import type { OrgDirectory } from "./orgDirectory";

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
  collection: string;
}

// A single outbox message classifies into at most one store mutation.
export type CrawlAction =
  | { kind: "delete"; spaceUri: string }
  | {
      kind: "upsert";
      spaceUri: string;
      docId: string;
      title: string;
      blob?: string;
    }
  | { kind: "none" };

// classify decides what the crawler should do with an outbox message: null
// value means the record was deleted; otherwise the doc's markdown or CRDT
// record is upserted.
export function classify(msg: OutboxMessage, parsed: ParsedRecordUri): CrawlAction {
  if (msg.value === null) {
    return { kind: "delete", spaceUri: parsed.spaceUri };
  }
  if (parsed.collection === MARKDOWN_COLLECTION) {
    const value = (msg.value ?? {}) as { title?: string };
    return {
      kind: "upsert",
      spaceUri: parsed.spaceUri,
      docId: parsed.skey,
      title: value.title || "Untitled",
    };
  }
  if (parsed.collection === CRDT_COLLECTION) {
    const value = (msg.value ?? {}) as { blob?: string };
    if (value.blob) {
      return { kind: "upsert", spaceUri: parsed.spaceUri, docId: "", title: "", blob: value.blob };
    }
    return { kind: "none" };
  }
  return { kind: "none" };
}

// parseSpaceRecordUri splits a space-record URI into the parts the crawler
// needs. Both the current form
// at://<owner>/space/<type>/<skey>/<repo>/<collection>/<rkey> and the legacy
// form ats://<owner>/<type>/<skey>/<repo>/<collection>/<rkey> are accepted;
// spaceUri is always returned in the current form. Returns undefined if the URI
// isn't a well-formed record URI.
export function parseSpaceRecordUri(uri: string): ParsedRecordUri | undefined {
  let parts: string[];
  if (uri.startsWith("at://")) {
    parts = uri.slice("at://".length).split("/");
    // Drop the literal "space" segment that separates owner from type.
    if (parts.length !== 7 || parts[1] !== "space") {
      return undefined;
    }
    parts.splice(1, 1);
  } else if (uri.startsWith("ats://")) {
    parts = uri.slice("ats://".length).split("/");
    if (parts.length !== 6) {
      return undefined;
    }
  } else {
    return undefined;
  }
  const [owner, type, skey, , collection] = parts;
  if (!owner || !type || !skey || !collection) {
    return undefined;
  }
  return {
    spaceUri: `at://${owner}/space/${type}/${skey}`,
    owner,
    type,
    skey,
    collection,
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
      ws.addEventListener("close", () => resolve());
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
    if (parsed.type === ORG_SPACE_TYPE) {
      // The space owner is the org whose membership changed; refetch it.
      await this.orgs.refresh(parsed.owner);
      return;
    }
    const action = classify(msg, parsed);
    if (action.kind === "delete") {
      this.meta.deleteDoc(action.spaceUri);
      this.crdt.deleteState(action.spaceUri);
      return;
    }
    if (action.kind === "upsert") {
      if (action.blob) {
        await this.crdt.upsertState(action.spaceUri, action.blob);
      } else {
        this.meta.upsertDoc({
          spaceUri: action.spaceUri,
          docId: action.docId,
          title: action.title,
        });
      }
    }
  }
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
