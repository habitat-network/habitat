import { and, eq, inArray, notInArray } from "drizzle-orm";
import type { BatchItem } from "drizzle-orm/batch";
import type { LexMap } from "@atproto/lex";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import { AtUri, SpaceRef } from "@atproto/syntax";
import { network } from "api";
import type { PromiseRepoBatch, PromiseSyncSink } from "internal/spaceSync";
import type { Db } from "@/db";
import { chats, messages } from "@/db/schema";

export const SESSION_COLLECTION = "network.habitat.brain.session";
export const MESSAGE_COLLECTION = "network.habitat.brain.message";

type ChatRow = typeof chats.$inferInsert;
type MessageRow = typeof messages.$inferInsert;

function parseTime(value: string): number {
  const ms = Date.parse(value);
  return Number.isNaN(ms) ? Date.now() : ms;
}

// toChatRow maps one synced record to its chats row, or undefined if it isn't
// a well-formed network.habitat.brain.session record. A malformed record is
// skipped rather than failing the batch: failing would wedge the space on it
// forever.
export function toChatRow(
  uri: string,
  cid: string,
  value: LexMap,
): ChatRow | undefined {
  const parsed = network.habitat.brain.session.$safeParse(value);
  if (!parsed.success) return undefined;
  const at = new AtUri(uri);
  if (!at.spaceDid || !at.spaceType || !at.skey) return undefined;
  return {
    space: new SpaceRef(at.spaceDid, at.spaceType, at.skey).toString(),
    cid,
    ownerDid: at.spaceDid,
    title: parsed.value.title,
    createdAt: parseTime(parsed.value.createdAt),
  };
}

// toMessageRow maps one synced record to its messages row, or undefined if it
// isn't a well-formed network.habitat.brain.message record (see toChatRow).
export function toMessageRow(
  uri: string,
  cid: string,
  value: LexMap,
): MessageRow | undefined {
  const parsed = network.habitat.brain.message.$safeParse(value);
  if (!parsed.success) return undefined;
  const at = new AtUri(uri);
  if (!at.spaceDid || !at.spaceType || !at.skey || !at.authorDid) {
    return undefined;
  }
  return {
    uri,
    cid,
    space: new SpaceRef(at.spaceDid, at.spaceType, at.skey).toString(),
    repo: at.authorDid,
    role: parsed.value.role,
    content: parsed.value.content,
    createdAt: parseTime(parsed.value.createdAt),
  };
}

function recordUri(
  space: SpaceRefString,
  did: DidString,
  collection: string,
  rkey: string,
): string {
  return `${space}/${did}/${collection}/${rkey}`;
}

export interface ChatSinkOptions {
  db: Db;
}

// ChatSink is internal/spaceSync's SyncSink for Brain: it keeps the chats and
// messages tables equal to the session and message records across every
// synced chat space.
//
// Each batch is applied as one D1 batch (a single transaction), so an
// aborted or failed apply leaves the tables as they were. Delivery is
// at-least-once; upserting by key makes a replay a no-op.
export class ChatSink implements PromiseSyncSink {
  constructor(private opts: ChatSinkOptions) {}

  async apply(batch: PromiseRepoBatch, signal: AbortSignal): Promise<void> {
    const { db } = this.opts;
    switch (batch._tag) {
      case "Ops": {
        const chatUpserts: ChatRow[] = [];
        const messageUpserts: MessageRow[] = [];
        const chatDeletes: string[] = [];
        const messageDeletes: string[] = [];
        for (const change of batch.changes) {
          if (change.collection === SESSION_COLLECTION) {
            if (change.cid === null) {
              const at = new AtUri(change.uri);
              if (at.spaceDid && at.spaceType && at.skey) {
                chatDeletes.push(
                  new SpaceRef(at.spaceDid, at.spaceType, at.skey).toString(),
                );
              }
            } else if (change.value) {
              const row = toChatRow(
                change.uri,
                change.cid.toString(),
                change.value,
              );
              if (row) chatUpserts.push(row);
            }
          } else if (change.collection === MESSAGE_COLLECTION) {
            if (change.cid === null) {
              messageDeletes.push(change.uri);
            } else if (change.value) {
              const row = toMessageRow(
                change.uri,
                change.cid.toString(),
                change.value,
              );
              if (row) messageUpserts.push(row);
            }
          }
        }
        signal.throwIfAborted();
        await this.write([
          ...(chatDeletes.length > 0
            ? [db.delete(chats).where(inArray(chats.space, chatDeletes))]
            : []),
          ...(messageDeletes.length > 0
            ? [db.delete(messages).where(inArray(messages.uri, messageDeletes))]
            : []),
          ...chatUpserts.map((row) => this.upsertChat(row)),
          ...messageUpserts.map((row) => this.upsertMessage(row)),
        ]);
        break;
      }
      case "Reset": {
        // Drain every record first: draining is what verifies them, and a
        // verification failure must fail the apply before anything changes.
        const chatUpserts: ChatRow[] = [];
        const messageUpserts: MessageRow[] = [];
        for await (const record of batch.records) {
          const uri = recordUri(
            batch.space,
            batch.did,
            record.collection,
            record.rkey,
          );
          const cid = record.cid.toString();
          if (record.collection === SESSION_COLLECTION) {
            const row = toChatRow(uri, cid, record.record);
            if (row) chatUpserts.push(row);
          } else if (record.collection === MESSAGE_COLLECTION) {
            const row = toMessageRow(uri, cid, record.record);
            if (row) messageUpserts.push(row);
          }
        }
        signal.throwIfAborted();
        // Everything this repo had in the space that the reset didn't list
        // is gone.
        const keep = messageUpserts.map((r) => r.uri);
        await db.batch([
          db
            .delete(messages)
            .where(
              and(
                eq(messages.space, batch.space),
                eq(messages.repo, batch.did),
                ...(keep.length > 0 ? [notInArray(messages.uri, keep)] : []),
              ),
            ),
          ...(chatUpserts.length === 0
            ? [db.delete(chats).where(eq(chats.space, batch.space))]
            : []),
          ...chatUpserts.map((row) => this.upsertChat(row)),
          ...messageUpserts.map((row) => this.upsertMessage(row)),
        ]);
        break;
      }
      case "RepoRemoved":
        await db.batch([
          db
            .delete(messages)
            .where(
              and(
                eq(messages.space, batch.space),
                eq(messages.repo, batch.did),
              ),
            ),
          db.delete(chats).where(eq(chats.space, batch.space)),
        ]);
        break;
      case "SpaceDeleted":
        await db.batch([
          db.delete(messages).where(eq(messages.space, batch.space)),
          db.delete(chats).where(eq(chats.space, batch.space)),
        ]);
        break;
    }
  }

  private upsertChat(row: ChatRow) {
    const { space: _, ...set } = row;
    return this.opts.db
      .insert(chats)
      .values(row)
      .onConflictDoUpdate({ target: chats.space, set });
  }

  private upsertMessage(row: MessageRow) {
    const { uri: _, ...set } = row;
    return this.opts.db
      .insert(messages)
      .values(row)
      .onConflictDoUpdate({ target: messages.uri, set });
  }

  private async write(statements: BatchItem<"sqlite">[]) {
    if (statements.length === 0) return;
    const [first, ...rest] = statements;
    await this.opts.db.batch([first, ...rest]);
  }
}
