import { DurableObject } from "cloudflare:workers";
import { drizzle, type DrizzleSqliteDODatabase } from "drizzle-orm/durable-sqlite";
import { and, eq } from "drizzle-orm";
import * as Y from "yjs";
import { docState, pendingFlush } from "./docRoomSchema";
import { frame } from "./frames";
import { SapClient } from "../sapClient";
import { renderDoc } from "../../render";
import { docByUri, getDb, upsertDoc } from "../../db";

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

export class DocRoom extends DurableObject<Env> {
  private db: DrizzleSqliteDODatabase;
  private ydoc = new Y.Doc();
  private id: DocIdentity | undefined;

  constructor(ctx: DurableObjectState, env: Env) {
    super(ctx, env);
    this.db = drizzle(ctx.storage);
    ctx.blockConcurrencyWhile(async () => {
      this.db.run(`CREATE TABLE IF NOT EXISTS doc_state (
        id INTEGER PRIMARY KEY, space_uri TEXT NOT NULL,
        owner_did TEXT, state BLOB, updated_at INTEGER NOT NULL)`);
      this.db.run(`CREATE TABLE IF NOT EXISTS pending_flush (
        kind TEXT NOT NULL, member_did TEXT NOT NULL,
        first_push_at INTEGER NOT NULL, idle_deadline INTEGER NOT NULL,
        PRIMARY KEY (kind, member_did))`);
      const [row] = await this.db.select().from(docState).where(eq(docState.id, 0)).limit(1);
      if (!row) return;
      this.id = { spaceUri: row.spaceUri, ownerDid: row.ownerDid ?? undefined };
      if (row.state) Y.applyUpdateV2(this.ydoc, new Uint8Array(row.state));
    });
  }

  async identity(): Promise<DocIdentity> {
    if (!this.id) throw new Error("DocRoom has no identity yet");
    return this.id;
  }

  async snapshot(): Promise<Uint8Array> {
    return Y.encodeStateAsUpdateV2(this.ydoc);
  }

  async applyEdit(id: DocIdentity, memberDid: string, update: Uint8Array): Promise<void> {
    await this.rememberIdentity(id);
    await this.mergeUpdate(update);
    await this.schedule("member", memberDid);
  }

  async applyRemote(id: DocIdentity, cid: string): Promise<void> {
    await this.rememberIdentity(id);
    // A putRecord'd network.habitat.docs.crdt record carries a blob
    // *reference*, not the update's bytes inline, so the bytes have to be
    // fetched separately via getBlob.
    const ownerDid = this.id?.ownerDid;
    if (!ownerDid) return;
    const bytes = await new SapClient(this.env, ownerDid).getBlob(id.spaceUri, cid);
    await this.mergeUpdate(bytes);
  }

  // rememberIdentity records spaceUri/ownerDid the first time any caller
  // supplies them, and never downgrades a known ownerDid back to undefined.
  private async rememberIdentity(id: DocIdentity): Promise<void> {
    const next: DocIdentity = {
      spaceUri: id.spaceUri,
      ownerDid: id.ownerDid ?? this.id?.ownerDid,
    };
    if (this.id?.spaceUri === next.spaceUri && this.id?.ownerDid === next.ownerDid) return;
    this.id = next;
    await this.persist();
  }

  // mergeUpdate applies a Yjs update to the in-memory doc and persists the
  // merged result. `this.ydoc` is loaded from storage in the constructor,
  // never a bare `new Y.Doc()` at merge time: `update` is an *incremental*
  // diff relative to the sender's state, so merging it into an empty doc
  // would reconstruct only that fragment and persisting it would destroy
  // everything that came before.
  protected async mergeUpdate(update: Uint8Array): Promise<void> {
    Y.applyUpdateV2(this.ydoc, update);
    await this.persist();
    this.broadcast();
    await this.schedule("owner", "");
  }

  private subscribers = new Set<ReadableStreamDefaultController<Uint8Array>>();

  async subscriberCount(): Promise<number> {
    return this.subscribers.size;
  }

  async fetch(req: Request): Promise<Response> {
    if (new URL(req.url).pathname !== "/subscribe") {
      return new Response("not found", { status: 404 });
    }
    const subscribers = this.subscribers;
    const snapshot = frame(Y.encodeStateAsUpdateV2(this.ydoc));
    let self: ReadableStreamDefaultController<Uint8Array>;
    const readable = new ReadableStream<Uint8Array>({
      start(controller) {
        self = controller;
        subscribers.add(controller);
        // Emitting the snapshot here, after registration, closes a race
        // today's code has: `store.mergedState` and `pubsub.subscribe` were
        // two separate steps, so a merge landing between them was lost.
        controller.enqueue(snapshot);
      },
      cancel() {
        // The subscriber's own stream cancellation is the only signal that
        // it's gone — a broadcast().enqueue() on a cancelled controller
        // would otherwise throw and take mergeUpdate down with it.
        subscribers.delete(self);
      },
    });
    return new Response(readable);
  }

  private broadcast(): void {
    const bytes = frame(Y.encodeStateAsUpdateV2(this.ydoc));
    for (const controller of this.subscribers) {
      try {
        controller.enqueue(bytes);
      } catch {
        this.subscribers.delete(controller);
      }
    }
  }

  // Policy carried over verbatim from today's DebounceQueue: flush 2s after
  // the last push, but never let more than 10s of edits go unwritten.
  private static readonly IDLE_MS = 2000;
  private static readonly MAX_WAIT_MS = 10000;

  private async schedule(kind: "member" | "owner", memberDid: string): Promise<void> {
    const now = Date.now();
    const [existing] = await this.db
      .select()
      .from(pendingFlush)
      .where(and(eq(pendingFlush.kind, kind), eq(pendingFlush.memberDid, memberDid)))
      .limit(1);
    await this.db
      .insert(pendingFlush)
      .values({
        kind,
        memberDid,
        firstPushAt: existing?.firstPushAt ?? now,
        idleDeadline: now + DocRoom.IDLE_MS,
      })
      .onConflictDoUpdate({
        target: [pendingFlush.kind, pendingFlush.memberDid],
        set: { idleDeadline: now + DocRoom.IDLE_MS },
      });
    await this.rearm();
  }

  // A DO has exactly one alarm, so it is always set to the earliest deadline
  // across every pending row: min(idleDeadline, firstPushAt + MAX_WAIT_MS).
  private async rearm(): Promise<void> {
    const rows = await this.db.select().from(pendingFlush);
    if (rows.length === 0) return;
    const next = Math.min(
      ...rows.map((r) => Math.min(r.idleDeadline, r.firstPushAt + DocRoom.MAX_WAIT_MS)),
    );
    await this.ctx.storage.setAlarm(next);
  }

  async alarm(): Promise<void> {
    const now = Date.now();
    const rows = await this.db.select().from(pendingFlush);
    for (const row of rows) {
      const due = Math.min(row.idleDeadline, row.firstPushAt + DocRoom.MAX_WAIT_MS);
      if (due > now) continue;
      await this.db
        .delete(pendingFlush)
        .where(and(eq(pendingFlush.kind, row.kind), eq(pendingFlush.memberDid, row.memberDid)));
      if (row.kind === "member") await this.flushMember(row.memberDid);
      if (row.kind === "owner") await this.republishCanonical();
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
    const blob = await client.uploadBlob(Y.encodeStateAsUpdateV2(this.ydoc), "application/octet-stream");
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
  // write, so it comes back through SapChannel looking like a fresh delta.
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
      Y.encodeStateAsUpdateV2(this.ydoc), "application/octet-stream",
    );
    await client.call("network.habitat.space.putRecord", "POST", {
      space: this.id.spaceUri, repo: ownerDid,
      collection: CRDT_COLLECTION, rkey: SELF, record: { blob: blob.blob },
    });
    await client.call("network.habitat.space.putRecord", "POST", {
      space: this.id.spaceUri, repo: ownerDid,
      collection: MARKDOWN_COLLECTION, rkey: SELF,
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
    const row = {
      id: 0,
      spaceUri: this.id.spaceUri,
      ownerDid: this.id.ownerDid ?? null,
      state: Buffer.from(Y.encodeStateAsUpdateV2(this.ydoc)),
      updatedAt: Date.now(),
    };
    await this.db.insert(docState).values(row).onConflictDoUpdate({ target: docState.id, set: row });
  }
}
