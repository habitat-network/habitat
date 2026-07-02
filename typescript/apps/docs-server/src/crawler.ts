import type { DerivedConfig } from "./config";
import type { PearClient } from "./pearClient";
import type { CrawlStore } from "./crawlStore";

// A doc is represented by its rendered-markdown record (which carries the
// title); the crawler keys doc discovery off this collection.
const MARKDOWN_COLLECTION = "network.habitat.docs.markdown";
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
  // The org DID that owns the space (used as the Habitat-Did when resolving the
  // doc's readers).
  owner: string;
  skey: string;
  collection: string;
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
  const [owner, type, skey, , collection] = parts;
  if (!owner || !type || !skey || !collection) {
    return undefined;
  }
  return {
    spaceUri: `ats://${owner}/${type}/${skey}`,
    owner,
    skey,
    collection,
  };
}

// Crawler subscribes to sap's outbox channel over the internal websocket, acks
// every message it receives, and persists the docs it discovers. For each doc
// it resolves the members that may read it (relationship.listSubjects) so
// listDocs can be filtered per-caller. It reconnects automatically; unacked
// messages are redelivered by sap on the next connection.
export class Crawler {
  private stopped = false;
  // Serializes message processing so acks are sent in delivery order and the
  // sqlite writes don't interleave.
  private queue: Promise<void> = Promise.resolve();

  constructor(
    private config: DerivedConfig,
    private pear: PearClient,
    private store: CrawlStore,
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
      const ws = new WebSocket(this.config.sapChannelUrl);
      ws.addEventListener("open", () => {
        console.log(`[crawler] connected to ${this.config.sapChannelUrl}`);
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
    if (!parsed || parsed.collection !== MARKDOWN_COLLECTION) {
      // Not a doc's markdown record; nothing to persist.
      return;
    }
    const value = (msg.value ?? {}) as { title?: string };
    this.store.upsertDoc({
      spaceUri: parsed.spaceUri,
      docId: parsed.skey,
      title: value.title || "Untitled",
    });
    try {
      const readers = await this.pear.listReaders(
        parsed.owner,
        parsed.spaceUri,
      );
      this.store.replaceReaders(parsed.spaceUri, readers);
    } catch (err) {
      // Keep any previously-stored readers; a later redelivery/update retries.
      console.error("[crawler] listSubjects failed", parsed.spaceUri, err);
    }
  }
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
