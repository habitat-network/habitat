import { and, eq, inArray, notInArray } from "drizzle-orm";
import {
  getBlobCidString,
  getBlobMime,
  getBlobSize,
  type LexMap,
} from "@atproto/lex";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import { AtUri, SpaceRef } from "@atproto/syntax";
import { network } from "api";
import type { PromiseRepoBatch, PromiseSyncSink } from "internal/spaceSync";
import type { Db } from "@/db";
import { files } from "@/db/schema";

export const FILE_COLLECTION = "network.habitat.drop.file";

type FileRow = typeof files.$inferInsert;

export interface FileSinkOptions {
  db: Db;
}

// toFileRow maps one synced record to its files row, or undefined if it isn't
// a well-formed network.habitat.drop.file record. A malformed record is
// skipped rather than failing the batch: the writer is the org itself, but a
// record we can't parse can't be listed or downloaded either way, and failing
// would wedge the space on it forever.
export function toFileRow(
  uri: string,
  cid: string,
  value: LexMap,
): FileRow | undefined {
  const parsed = network.habitat.drop.file.$safeParse(value);
  if (!parsed.success) return undefined;
  const record = parsed.value;
  const at = new AtUri(uri);
  if (!at.spaceDid || !at.spaceType || !at.skey || !at.authorDid) {
    return undefined;
  }
  const created = Date.parse(record.createdAt);
  return {
    uri,
    cid,
    space: new SpaceRef(at.spaceDid, at.spaceType, at.skey).toString(),
    repo: at.authorDid,
    orgDid: at.spaceDid,
    name: record.name,
    blobCid: getBlobCidString(record.file),
    mimeType: getBlobMime(record.file) || "application/octet-stream",
    size: getBlobSize(record.file) ?? 0,
    uploadedBy: record.uploadedBy ?? null,
    createdAt: Number.isNaN(created) ? Date.now() : created,
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

// FileSink is internal/spaceSync's SyncSink for Drop: it keeps the files
// table equal to the set of network.habitat.drop.file records across every
// synced space. Blobs aren't copied: downloads read them from the org's
// space host (see src/routes/api.download.ts).
//
// Each batch is applied as one D1 batch (a single transaction), so an
// aborted or failed apply leaves the table as it was. Delivery is
// at-least-once; upserting by uri makes a replay a no-op.
export class FileSink implements PromiseSyncSink {
  constructor(private opts: FileSinkOptions) {}

  async apply(batch: PromiseRepoBatch, signal: AbortSignal): Promise<void> {
    const { db } = this.opts;
    switch (batch._tag) {
      case "Ops": {
        const upserts: FileRow[] = [];
        const deletes: string[] = [];
        for (const change of batch.changes) {
          if (change.collection !== FILE_COLLECTION) continue;
          if (change.cid === null) {
            deletes.push(change.uri);
          } else if (change.value) {
            const row = toFileRow(
              change.uri,
              change.cid.toString(),
              change.value,
            );
            if (row) upserts.push(row);
          }
        }
        signal.throwIfAborted();
        await this.write(upserts, deletes);
        break;
      }
      case "Reset": {
        // Drain every record first: draining is what verifies them, and a
        // verification failure must fail the apply before anything changes.
        const upserts: FileRow[] = [];
        for await (const record of batch.records) {
          if (record.collection !== FILE_COLLECTION) continue;
          const uri = recordUri(
            batch.space,
            batch.did,
            record.collection,
            record.rkey,
          );
          const row = toFileRow(uri, record.cid.toString(), record.record);
          if (row) upserts.push(row);
        }
        signal.throwIfAborted();
        // Everything this repo had in the space that the reset didn't list
        // is gone.
        const keep = upserts.map((r) => r.uri);
        await db.batch([
          db
            .delete(files)
            .where(
              and(
                eq(files.space, batch.space),
                eq(files.repo, batch.did),
                ...(keep.length > 0 ? [notInArray(files.uri, keep)] : []),
              ),
            ),
          ...upserts.map((row) => this.upsert(row)),
        ]);
        break;
      }
      case "RepoRemoved":
        await db
          .delete(files)
          .where(and(eq(files.space, batch.space), eq(files.repo, batch.did)));
        break;
      case "SpaceDeleted":
        await db.delete(files).where(eq(files.space, batch.space));
        break;
    }
  }

  private upsert(row: FileRow) {
    const { uri: _, ...set } = row;
    return this.opts.db
      .insert(files)
      .values(row)
      .onConflictDoUpdate({ target: files.uri, set });
  }

  private async write(upserts: FileRow[], deletes: string[]) {
    const { db } = this.opts;
    const statements = [
      ...(deletes.length > 0
        ? [db.delete(files).where(inArray(files.uri, deletes))]
        : []),
      ...upserts.map((row) => this.upsert(row)),
    ];
    if (statements.length === 0) return;
    const [first, ...rest] = statements;
    await db.batch([first, ...rest]);
  }
}
