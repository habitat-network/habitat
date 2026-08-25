import { DurableObject } from "cloudflare:workers";
import * as Y from "yjs";
import * as encoding from "lib0/encoding";
import * as decoding from "lib0/decoding";
import * as syncProtocol from "y-protocols/sync";
import * as awarenessProtocol from "y-protocols/awareness";
import { SapClient } from "../sapClient";
import { renderDoc } from "../../render";
import { docByUri, getDb, upsertDoc } from "../../db";

// The outer wire-protocol byte y-websocket's WebsocketProvider (client)
// wraps every message in — see y-websocket/src/y-websocket.js. This DO
// implements the two message types a client-authoritative-server setup
// actually needs; messageAuth/messageQueryAwareness (2/3) go unhandled,
// matching the reference server's own scope.
const MESSAGE_SYNC = 0;
const MESSAGE_AWARENESS = 1;

const CRDT_COLLECTION = "network.habitat.docs.crdt";
const MARKDOWN_COLLECTION = "network.habitat.docs.markdown";
const SELF = "self";

export interface DocIdentity {
  spaceUri: string;
  // Optional: a doc shared with this member may not be in this deployment's
  // D1 index, and the member-edit flush writes to the member's own repo and
  // never needs the owner. The owner-republish flush is skipped while this
  // is unknown, mirroring today's `republishCanonical`'s `if (!doc) return;`.
  ownerDid?: string;
}

// This room's storage needs are a single "current state" value plus a small,
// prefix-grouped set of pending-flush entries — the shape Cloudflare's own
// storage-access guidance recommends the key-value API for, over ctx.storage.sql
// (see docs/durable-objects/best-practices/access-durable-objects-storage).
// DocRoom is still a SQLite-backed class (wrangler.jsonc's new_sqlite_classes),
// so this isn't a different storage engine or a smaller size ceiling than SQL
// would have had — ctx.storage.get/put on a SQLite-backed DO reads/writes the
// same per-object database, just without a query builder in front of it.
const DOC_KEY = "doc";
const PENDING_PREFIX = "pending:";

interface StoredDoc {
  spaceUri: string;
  ownerDid?: string;
  state: Uint8Array;
  updatedAt: number;
}

interface PendingFlush {
  kind: "member" | "owner";
  memberDid: string;
  firstPushAt: number;
  idleDeadline: number;
}

// The pending-flush key only needs to be unique per (kind, memberDid) — it's
// never parsed back apart, so a DID's own colons in memberDid can't cause
// ambiguity the way splitting the key on ":" would.
function pendingKey(kind: "member" | "owner", memberDid: string): string {
  return `${PENDING_PREFIX}${kind}:${memberDid}`;
}

export class DocRoom extends DurableObject<Env> {
  private ydoc = new Y.Doc();
  private id: DocIdentity | undefined;
  // Ephemeral (cursor-position-style) presence, not persisted — a
  // hibernation eviction re-running the constructor is expected to reset
  // it, which is fine: presence doesn't need to survive that the way the
  // document content does.
  private awareness = new awarenessProtocol.Awareness(this.ydoc);

  constructor(ctx: DurableObjectState, env: Env) {
    super(ctx, env);
    ctx.blockConcurrencyWhile(async () => {
      const doc = await ctx.storage.get<StoredDoc>(DOC_KEY);
      if (doc) {
        this.id = { spaceUri: doc.spaceUri, ownerDid: doc.ownerDid };
        Y.applyUpdateV2(this.ydoc, doc.state);
      }
      // Registered only after the load above applies: an "updateV2"
      // listener attached earlier would fire onUpdate for this restore on
      // every cold start/hibernation wake, needlessly re-scheduling a
      // member/owner flush (and its SapClient upload) for content that
      // hasn't actually changed.
      //
      // Pushed onto `pending` rather than awaited here directly: Yjs's
      // event emitter invokes listeners synchronously and doesn't await
      // them, so a bare `void this.onUpdate(...)` would let onUpdate's I/O
      // keep running after the RPC/webSocketMessage call that triggered
      // the update has already returned — which Workers tears down the
      // I/O context for, surfacing as "Network connection lost" from
      // whatever storage/fetch call was still in flight. Every call site
      // that can trigger a mutation (applyEdit, applyRemote,
      // webSocketMessage's syncProtocol.readSyncMessage) awaits
      // flushPending() immediately after, so this object's own call stack
      // is what actually stays open until the side effects finish.
      this.ydoc.on("updateV2", (update: Uint8Array, origin: unknown) => {
        this.pending.push(this.onUpdate(update, origin));
      });
    });
  }

  private pending: Promise<void>[] = [];

  private async flushPending(): Promise<void> {
    const batch = this.pending;
    this.pending = [];
    await Promise.all(batch);
  }

  async identity(): Promise<DocIdentity> {
    if (!this.id) throw new Error("DocRoom has no identity yet");
    return this.id;
  }

  async snapshot(): Promise<Uint8Array> {
    return Y.encodeStateAsUpdateV2(this.ydoc);
  }

  // applyEdit is the identity-establishing entry point for a member edit
  // applied directly (as opposed to one arriving over an already-open
  // WebSocket, where identity was established at connect time — see
  // fetch()). Kept as its own RPC method rather than folded away: tests
  // exercising the debounced flush/republish behavior use it to simulate
  // an edit without needing a live socket.
  async applyEdit(
    id: DocIdentity,
    memberDid: string,
    update: Uint8Array,
  ): Promise<void> {
    await this.rememberIdentity(id);
    Y.applyUpdateV2(this.ydoc, update, memberDid);
    await this.flushPending();
  }

  // seedIdentity records spaceUri/ownerDid without touching the document.
  // createDoc calls this so the owner-republish alarm knows the owner before
  // the webhook (src/server/webhook.ts) would supply it. It is deliberately not an
  // `applyEdit` with an empty update: there is no such thing as a
  // zero-length Yjs update — `Y.applyUpdateV2` throws "Unexpected end of
  // array" on `new Uint8Array(0)` — and even a well-formed empty update
  // (`Y.encodeStateAsUpdateV2(new Y.Doc())`) would schedule a member and an
  // owner flush, publishing an empty blob to two repos for a doc nobody has
  // typed in yet.
  async seedIdentity(id: DocIdentity): Promise<void> {
    await this.rememberIdentity(id);
  }

  async applyRemote(id: DocIdentity, cid: string): Promise<void> {
    await this.rememberIdentity(id);
    // A putRecord'd network.habitat.docs.crdt record carries a blob
    // *reference*, not the update's bytes inline, so the bytes have to be
    // fetched separately via getBlob.
    const ownerDid = this.id?.ownerDid;
    if (!ownerDid) return;
    const bytes = await new SapClient(this.env, ownerDid).getBlob(
      id.spaceUri,
      cid,
    );
    // No transaction origin: this update is already the merged/canonical
    // state as sap has it, not a specific member's edit, so onUpdate below
    // must not attribute it to (and re-flush it back into) anyone's repo —
    // only the owner-republish scheduling applies.
    Y.applyUpdateV2(this.ydoc, bytes);
    await this.flushPending();
  }

  // rememberIdentity records spaceUri/ownerDid the first time any caller
  // supplies them, and never downgrades a known ownerDid back to undefined.
  private async rememberIdentity(id: DocIdentity): Promise<void> {
    const next: DocIdentity = {
      spaceUri: id.spaceUri,
      ownerDid: id.ownerDid ?? this.id?.ownerDid,
    };
    if (
      this.id?.spaceUri === next.spaceUri &&
      this.id?.ownerDid === next.ownerDid
    )
      return;
    this.id = next;
    await this.persist();
  }

  // onUpdate is the single funnel every doc mutation runs through,
  // regardless of source — the ydoc "updateV2" listener registered in the
  // constructor, not a method any caller invokes directly. That's what
  // makes it safe for applyEdit and applyRemote to just call
  // Y.applyUpdateV2 themselves: persistence, live broadcast, and the
  // debounced-flush scheduling all happen exactly once, here, however the
  // update arrived.
  //
  // origin tells us who to attribute the edit to: a WebSocket (a live
  // subscriber's own edit, applied via webSocketMessage's sync-protocol
  // handling below) or a plain memberDid string (applyEdit's direct RPC
  // path) both schedule a member flush; anything else (applyRemote's
  // unset origin, or the constructor's own initial-load apply — though
  // that one never reaches here, see the constructor's comment) does not.
  private async onUpdate(update: Uint8Array, origin: unknown): Promise<void> {
    await this.persist();
    this.broadcast(update, origin instanceof WebSocket ? origin : undefined);
    const memberDid =
      origin instanceof WebSocket
        ? ((origin.deserializeAttachment() as string | null) ?? undefined)
        : typeof origin === "string"
          ? origin
          : undefined;
    if (memberDid) await this.schedule("member", memberDid);
    await this.schedule("owner", "");
  }

  // fetch handles a WebSocket upgrade for a live subscriber, accepted via
  // the Hibernation API (ctx.acceptWebSocket) rather than plain ws.accept():
  // that's what lets this object be evicted from memory between edits while
  // an editor's tab sits open, instead of being billed Durable Object
  // "duration" continuously for as long as the connection is held — the
  // same wall-clock-not-CPU-time cost that made SapChannel's WebSocket to
  // sap expensive (see src/server/webhook.ts's replacement of that). The
  // Worker route that forwards here (src/routes/ws.$docId.ts) has already
  // done the auth/access check and set these two headers; DocRoom itself
  // has no ACL.
  //
  // Nothing is sent here: y-websocket's WebsocketProvider (the client)
  // always sends sync step 1 as soon as the socket opens, and this room's
  // reply — carrying its full current state — happens naturally from
  // webSocketMessage's y-protocols/sync handling below. That's the
  // client-initiated handshake the protocol is documented to expect (see
  // y-protocols/sync.js's module comment); the room doesn't need its own
  // "send the snapshot on connect" step the way the old ReadableStream
  // version did.
  async fetch(request: Request): Promise<Response> {
    if (request.headers.get("Upgrade") !== "websocket") {
      return new Response("expected a websocket upgrade", { status: 426 });
    }
    const docId = request.headers.get("X-Chalk-Doc-Id");
    const memberDid = request.headers.get("X-Chalk-Member-Did");
    if (!docId || !memberDid) {
      return new Response("missing doc/member identity", { status: 400 });
    }
    await this.rememberIdentity({ spaceUri: docId });

    const pair = new WebSocketPair();
    const [client, server] = Object.values(pair);
    this.ctx.acceptWebSocket(server);
    // Persists through hibernation (per the Hibernation API's own
    // serializeAttachment guarantee), so onUpdate can still attribute an
    // edit to the right member's repo after this object has been evicted
    // and woken back up.
    server.serializeAttachment(memberDid);

    return new Response(null, { status: 101, webSocket: client });
  }

  // webSocketMessage is the Hibernation API's per-message handler — see
  // fetch()'s comment on why nothing here keeps the object resident
  // between messages. Every message is y-websocket's own two-level wire
  // format: an outer MESSAGE_SYNC/MESSAGE_AWARENESS byte, then (for sync)
  // y-protocols/sync's own inner step1/step2/update framing.
  async webSocketMessage(
    ws: WebSocket,
    message: ArrayBuffer | string,
  ): Promise<void> {
    if (typeof message === "string") return; // the protocol is binary-only
    const decoder = decoding.createDecoder(new Uint8Array(message));
    const messageType = decoding.readVarUint(decoder);
    if (messageType === MESSAGE_SYNC) {
      const encoder = encoding.createEncoder();
      encoding.writeVarUint(encoder, MESSAGE_SYNC);
      // readSyncMessage does the real work: for the client's initial sync
      // step 1 (its — empty — state vector), it writes this room's full
      // state into encoder as a step 2 reply; for a step 2/update (an
      // actual edit), it applies it to this.ydoc directly, which is what
      // fires the "updateV2" listener (onUpdate, above) with `ws` as the
      // transaction origin — synchronously, but its actual persist/
      // broadcast/schedule work is only awaited below via flushPending(),
      // for the same reason applyEdit/applyRemote do.
      syncProtocol.readSyncMessage(decoder, encoder, this.ydoc, ws);
      if (encoding.length(encoder) > 1) ws.send(encoding.toUint8Array(encoder));
      await this.flushPending();
    } else if (messageType === MESSAGE_AWARENESS) {
      awarenessProtocol.applyAwarenessUpdate(
        this.awareness,
        decoding.readVarUint8Array(decoder),
        ws,
      );
      // Relayed as-is (not re-encoded): the awareness protocol's own
      // update payload is peer-to-peer data this room doesn't otherwise
      // need to inspect, so the already-correctly-framed incoming bytes
      // are exactly what every other subscriber needs to receive too.
      const raw = new Uint8Array(message);
      for (const other of this.ctx.getWebSockets()) {
        if (other === ws) continue;
        try {
          other.send(raw);
        } catch {
          // See broadcast()'s comment: getWebSockets() self-corrects.
        }
      }
    }
  }

  // broadcast reads ctx.getWebSockets() fresh on every call rather than
  // keeping an in-memory subscriber list: a hibernation eviction clears
  // this object's JS heap (including any such list) but not the runtime's
  // own record of which sockets are attached, so getWebSockets() is the
  // only source of truth that survives hibernation. exceptOrigin skips the
  // socket an edit came from — it already has this update applied locally
  // (that's how Yjs's own optimistic local editing works), so echoing it
  // back would just be wasted bandwidth, not incorrect.
  private broadcast(
    update: Uint8Array,
    exceptOrigin: WebSocket | undefined,
  ): void {
    const encoder = encoding.createEncoder();
    encoding.writeVarUint(encoder, MESSAGE_SYNC);
    syncProtocol.writeUpdate(encoder, update);
    const bytes = encoding.toUint8Array(encoder);
    for (const ws of this.ctx.getWebSockets()) {
      if (ws === exceptOrigin) continue;
      try {
        ws.send(bytes);
      } catch {
        // A send to a socket the runtime hasn't yet reported as closed can
        // still throw; getWebSockets() excludes it on the next call
        // regardless, so there's nothing to clean up here.
      }
    }
  }

  // Policy carried over verbatim from today's DebounceQueue: flush 2s after
  // the last push, but never let more than 10s of edits go unwritten.
  private static readonly IDLE_MS = 2000;
  private static readonly MAX_WAIT_MS = 10000;

  private async schedule(
    kind: "member" | "owner",
    memberDid: string,
  ): Promise<void> {
    const key = pendingKey(kind, memberDid);
    const now = Date.now();
    const existing = await this.ctx.storage.get<PendingFlush>(key);
    await this.ctx.storage.put<PendingFlush>(key, {
      kind,
      memberDid,
      firstPushAt: existing?.firstPushAt ?? now,
      idleDeadline: now + DocRoom.IDLE_MS,
    });
    await this.rearm();
  }

  // A DO has exactly one alarm, so it is always set to the earliest deadline
  // across every pending entry: min(idleDeadline, firstPushAt + MAX_WAIT_MS).
  private async rearm(): Promise<void> {
    const pending = await this.ctx.storage.list<PendingFlush>({
      prefix: PENDING_PREFIX,
    });
    if (pending.size === 0) return;
    const next = Math.min(
      ...[...pending.values()].map((p) =>
        Math.min(p.idleDeadline, p.firstPushAt + DocRoom.MAX_WAIT_MS),
      ),
    );
    await this.ctx.storage.setAlarm(next);
  }

  async alarm(): Promise<void> {
    const now = Date.now();
    const pending = await this.ctx.storage.list<PendingFlush>({
      prefix: PENDING_PREFIX,
    });
    for (const [key, entry] of pending) {
      const due = Math.min(
        entry.idleDeadline,
        entry.firstPushAt + DocRoom.MAX_WAIT_MS,
      );
      if (due > now) continue;
      await this.ctx.storage.delete(key);
      if (entry.kind === "member") await this.flushMember(entry.memberDid);
      if (entry.kind === "owner") await this.republishCanonical();
    }
    await this.rearm();
  }

  // flushMember writes the merged state to this member's own repo. It uploads
  // `this.ydoc` — the full merged snapshot, not the queued diffs — because
  // each queued update is an incremental diff, meaningful only relative to the
  // state it was generated against; uploading diffs alone would publish a blob
  // missing everything that predates this batch.
  private async flushMember(memberDid: string): Promise<void> {
    if (!this.id) return;
    const client = new SapClient(this.env, memberDid);
    const blob = await client.uploadBlob(
      Y.encodeStateAsUpdateV2(this.ydoc),
      "application/octet-stream",
    );
    await client.call("network.habitat.space.putRecord", "POST", {
      space: this.id.spaceUri,
      repo: memberDid,
      collection: CRDT_COLLECTION,
      rkey: SELF,
      record: { blob: blob.blob },
    });
  }

  // republishCanonical writes the merged markdown + CRDT snapshot back under
  // the doc owner's own repo. This is itself a network.habitat.docs.crdt
  // write, so it comes back through the webhook looking like a fresh delta.
  // It does not loop: pear's PutRecord skips advancing the record (and the
  // notifyWrite it would cause) when a write does not change the record's
  // CID (see internal/spaces/store.go), so a no-op republish produces no
  // outbox event.
  private async republishCanonical(): Promise<void> {
    const ownerDid = this.id?.ownerDid;
    if (!this.id || !ownerDid) return;
    const rendered = renderDoc(this.ydoc);
    const client = new SapClient(this.env, ownerDid);
    const blob = await client.uploadBlob(
      Y.encodeStateAsUpdateV2(this.ydoc),
      "application/octet-stream",
    );
    await client.call("network.habitat.space.putRecord", "POST", {
      space: this.id.spaceUri,
      repo: ownerDid,
      collection: CRDT_COLLECTION,
      rkey: SELF,
      record: { blob: blob.blob },
    });
    await client.call("network.habitat.space.putRecord", "POST", {
      space: this.id.spaceUri,
      repo: ownerDid,
      collection: MARKDOWN_COLLECTION,
      rkey: SELF,
      record: { title: rendered.title, content: rendered.markdown },
    });
    const existing = await docByUri(getDb(this.env), this.id.spaceUri);
    await upsertDoc(getDb(this.env), {
      spaceUri: this.id.spaceUri,
      docId: existing?.docId ?? this.id.spaceUri,
      ownerDid,
      title: rendered.title,
    });
  }

  private async persist(): Promise<void> {
    if (!this.id) return;
    await this.ctx.storage.put<StoredDoc>(DOC_KEY, {
      spaceUri: this.id.spaceUri,
      ownerDid: this.id.ownerDid,
      state: Y.encodeStateAsUpdateV2(this.ydoc),
      updatedAt: Date.now(),
    });
  }
}
