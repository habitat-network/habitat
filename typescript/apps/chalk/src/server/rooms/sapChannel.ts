import { DurableObject } from "cloudflare:workers";
import { docByUri, getDb } from "../../db";
import { parseSpaceRecordUri, type OutboxMessage } from "../spaceUri";

const CRDT_COLLECTION = "network.habitat.docs.crdt";
const RECONNECT_DELAY_MS = 2000;

export class SapChannel extends DurableObject<Env> {
  private ws: WebSocket | undefined;

  // ensureConnected is idempotent: it is called both by the cron trigger and
  // by the reconnect alarm, and no-ops while a socket is live.
  async ensureConnected(): Promise<void> {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) return;
    const base = this.env.CHALK_SAP_INTERNAL_URL;
    if (!base) throw new Error("CHALK_SAP_INTERNAL_URL is not set");
    const res = await fetch(`${base.replace(/^http/, "ws")}/channel`, {
      headers: { Upgrade: "websocket" },
    });
    const ws = res.webSocket;
    if (!ws) throw new Error("sap did not upgrade to a websocket");
    ws.accept();
    this.ws = ws;
    ws.addEventListener("message", (ev) => {
      void this.onMessage(
        ws,
        typeof ev.data === "string" ? ev.data : String(ev.data),
      );
    });
    ws.addEventListener("close", () => {
      if (this.ws === ws) this.ws = undefined;
      void this.ctx.storage.setAlarm(Date.now() + RECONNECT_DELAY_MS);
    });
  }

  async alarm(): Promise<void> {
    await this.ensureConnected();
  }

  private async onMessage(ws: WebSocket, data: string): Promise<void> {
    let msg: OutboxMessage;
    try {
      msg = JSON.parse(data) as OutboxMessage;
    } catch (err) {
      console.error("[sapChannel] malformed message", data, err);
      return;
    }
    try {
      await this.handleOutboxMessage(msg);
    } catch (err) {
      // Do not ack: sap redelivers, and re-applying a Yjs update already
      // merged is a no-op, so at-least-once is safe here.
      console.error("[sapChannel] handle message", err);
      return;
    }
    if (ws.readyState === WebSocket.OPEN)
      ws.send(JSON.stringify({ id: msg.id }));
  }

  // handleOutboxMessage routes one outbox event. Messages this deliberately
  // ignores (wrong collection, unknown doc, missing blob ref) return normally
  // so they are acked immediately — one uninteresting message must not be
  // able to wedge the outbox.
  async handleOutboxMessage(msg: OutboxMessage): Promise<void> {
    const parsed = parseSpaceRecordUri(msg.uri);
    if (!parsed || parsed.collection !== CRDT_COLLECTION) return;
    const value = (msg.value ?? {}) as { blob?: { ref?: { $link?: string } } };
    const cid = value.blob?.ref?.$link;
    if (!cid) return;
    const doc = await docByUri(getDb(this.env), parsed.spaceUri);
    if (!doc) return; // this deployment does not know this doc
    const stub = this.env.DOC.get(this.env.DOC.idFromName(parsed.spaceUri));
    await stub.applyRemote(
      { spaceUri: parsed.spaceUri, ownerDid: doc.ownerDid },
      cid,
    );
  }
}
