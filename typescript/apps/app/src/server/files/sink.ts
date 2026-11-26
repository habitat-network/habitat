import { eq } from "drizzle-orm";
import {
  getBlobCidString,
  getBlobMime,
  getBlobSize,
  type LexMap,
} from "@atproto/lex";
import { AtUri, SpaceRef } from "@atproto/syntax";
import { network } from "api";
import { Effect } from "effect";
import type { Domain } from "../stream/domain";
import type { Db } from "@/db";
import { files } from "@/db/schema";

// Records written before the spaces alpha moved space URIs to the at:space/
// form still embed the older at://{did}/space/... form, which only lenient
// validation accepts. Goes with the migration to the new form.
const LENIENT = { strict: false };

export const FILE_COLLECTION = network.habitat.drop.file.$nsid;

type FileRow = typeof files.$inferInsert;

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
  const parsed = network.habitat.drop.file.$safeParse(value, LENIENT);
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

// filesDomain keeps the files table equal to the set of
// network.habitat.drop.file records across every streamed file space. Blobs
// aren't copied: downloads read them from pear (see
// src/routes/api.download.ts). Each event is one D1 statement, upserted by
// uri, so a replay is a no-op.
export function filesDomain(db: Db): Domain {
  return {
    collections: [
      {
        collection: FILE_COLLECTION,
        handlers: {
          put: (event) =>
            Effect.tryPromise(async () => {
              const row = toFileRow(event.uri, event.cid, event.record);
              if (!row) return;
              const { uri: _, ...set } = row;
              await db
                .insert(files)
                .values(row)
                .onConflictDoUpdate({ target: files.uri, set });
            }),
          del: (event) =>
            Effect.tryPromise(async () => {
              await db.delete(files).where(eq(files.uri, event.uri));
            }),
        },
      },
    ],
    spaceDeleted: (event) =>
      Effect.tryPromise(async () => {
        await db.delete(files).where(eq(files.space, event.space));
      }),
  };
}
