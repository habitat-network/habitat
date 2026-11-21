import { and, eq, inArray } from "drizzle-orm";
import {
  getBlobCidString,
  getBlobMime,
  getBlobSize,
  type LexMap,
} from "@atproto/lex";
import { AtUri, SpaceRef } from "@atproto/syntax";
import { network } from "api";
import type { RepoBatch, SyncSink } from "../stream/batch";
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

// FileSink is the SyncSink for Drop: it keeps the files
// table equal to the set of network.habitat.drop.file records across every
// streamed space. Blobs aren't copied: downloads read them from pear (see src/routes/api.download.ts).
//
// Each batch is applied as one D1 batch (a single transaction), so an
// aborted or failed apply leaves the table as it was. Delivery is
// at-least-once; upserting by uri makes a replay a no-op.
export class FileSink implements SyncSink {
  constructor(private opts: FileSinkOptions) {}

  async apply(batch: RepoBatch, signal: AbortSignal): Promise<void> {
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
