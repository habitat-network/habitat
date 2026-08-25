import { DurableObject } from "cloudflare:workers";
import * as Y from "yjs";
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

  constructor(ctx: DurableObjectState, env: Env) {
    super(ctx, env);
    ctx.blockConcurrencyWhile(async () => {
      const doc = await ctx.storage.get<StoredDoc>(DOC_KEY);
      if (!doc) return;
      this.id = { spaceUri: doc.spaceUri, ownerDid: doc.ownerDid };
      Y.applyUpdateV2(this.ydoc, doc.state);
    });
  }

  async identity(): Promise<DocIdentity> {
    if (!this.id) throw new Error("DocRoom has no identity yet");
    return this.id;
  }

  async snapshot(): Promise<Uint8Array> {
    return Y.encodeStateAsUpdateV2(this.ydoc);
  }

  async applyEdit(
    id: DocIdentity,
    memberDid: string,
    update: Uint8Array,
  ): Promise<void> {
    await this.rememberIdentity(id);
    await this.mergeUpdate(update);
    await this.schedule("member", memberDid);
  }

  // seedIdentity records spaceUri/ownerDid without touching the document.
  // createDoc calls this so the owner-republish alarm knows the owner before
  // any SapChannel delivery would supply it. It is deliberately not an
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
    await this.mergeUpdate(bytes);
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

  // subscribe returns a live stream of length-prefixed Yjs V2 update frames
  // (see ./frames): the current snapshot first, then every subsequent
  // merge. Returned directly as an RPC value — Workers RPC streams
  // ReadableStream return values with proper flow control and transfers
  // stream ownership to the caller, so this needs no `fetch()`/synthetic
  // URL indirection the way an HTTP-only Worker would.
  async subscribe(): Promise<ReadableStream<Uint8Array>> {
    const subscribers = this.subscribers;
    const snapshot = frame(Y.encodeStateAsUpdateV2(this.ydoc));
    let self: ReadableStreamDefaultController<Uint8Array>;
    return new ReadableStream<Uint8Array>({
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
