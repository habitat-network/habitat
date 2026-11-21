import { and, eq, inArray, notInArray } from "drizzle-orm";
import { getBlobCidString, getBlobSize, type LexMap } from "@atproto/lex";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import { AtUri, SpaceRef } from "@atproto/syntax";
import { network } from "api";
import type { PromiseRepoBatch, PromiseSyncSink } from "internal/spaceSync";
import type { Db } from "@/db";
import { apps } from "@/db/schema";
import { hostLabel } from "@/lib/apps";

export const APP_COLLECTION = "network.habitat.apps.app";

type AppRow = typeof apps.$inferInsert;

function time(value: string | undefined, fallback: number): number {
  const t = value ? Date.parse(value) : Number.NaN;
  return Number.isNaN(t) ? fallback : t;
}

// toAppRow maps one synced record to its apps row, or undefined if it isn't
// a well-formed network.habitat.apps.app record written by the space's own
// group. A malformed record is skipped rather than failing the batch (see
// files/sink.ts). Records written by anyone but the group are skipped too:
// an app's code runs at the group's URL, so only the group decides it.
export function toAppRow(
  uri: string,
  cid: string,
  value: LexMap,
): AppRow | undefined {
  const parsed = network.habitat.apps.app.$safeParse(value);
  if (!parsed.success) return undefined;
  const record = parsed.value;
  const at = new AtUri(uri);
  if (!at.spaceDid || !at.spaceType || !at.skey || !at.authorDid) {
    return undefined;
  }
  if (at.authorDid !== at.spaceDid) return undefined;
  const host = hostLabel(record.slug, at.skey);
  if (!host) return undefined;
  const now = Date.now();
  const createdAt = time(record.createdAt, now);
  return {
    uri,
    cid,
    space: new SpaceRef(at.spaceDid, at.spaceType, at.skey).toString(),
    repo: at.authorDid,
    orgDid: at.spaceDid,
    host,
    name: record.name,
    slug: record.slug,
    description: record.description ?? null,
    entrypoint: record.entrypoint,
    codeCid: getBlobCidString(record.code),
    codeSize: getBlobSize(record.code) ?? 0,
    version: record.version,
    compatibilityDate: record.compatibilityDate,
    createdBy: record.createdBy ?? null,
    updatedBy: record.updatedBy ?? null,
    createdAt,
    updatedAt: time(record.updatedAt, createdAt),
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

// AppRecordSink keeps the apps table equal to the set of
// network.habitat.apps.app records across every synced app space. The code
// isn't copied: the runner reads it from the group's space host when an
// app's isolate starts (see runner.ts). Applied the same way as FileSink:
// one D1 batch per apply, upserts by uri so a replay is a no-op.
export class AppRecordSink implements PromiseSyncSink {
  constructor(private opts: { db: Db }) {}

  async apply(batch: PromiseRepoBatch, signal: AbortSignal): Promise<void> {
    const { db } = this.opts;
    switch (batch._tag) {
      case "Ops": {
        const upserts: AppRow[] = [];
        const deletes: string[] = [];
        for (const change of batch.changes) {
          if (change.collection !== APP_COLLECTION) continue;
          if (change.cid === null) {
            deletes.push(change.uri);
          } else if (change.value) {
            const row = toAppRow(
              change.uri,
              change.cid.toString(),
              change.value,
            );
            // A record that no longer parses takes the app down rather than
            // leaving its last good version running.
            if (row) upserts.push(row);
            else deletes.push(change.uri);
          }
        }
        signal.throwIfAborted();
        await this.write(upserts, deletes);
        break;
      }
      case "Reset": {
        const upserts: AppRow[] = [];
        for await (const record of batch.records) {
          if (record.collection !== APP_COLLECTION) continue;
          const uri = recordUri(
            batch.space,
            batch.did,
            record.collection,
            record.rkey,
          );
          const row = toAppRow(uri, record.cid.toString(), record.record);
          if (row) upserts.push(row);
        }
        signal.throwIfAborted();
        const keep = upserts.map((r) => r.uri);
        await db.batch([
          db
            .delete(apps)
            .where(
              and(
                eq(apps.space, batch.space),
                eq(apps.repo, batch.did),
                ...(keep.length > 0 ? [notInArray(apps.uri, keep)] : []),
              ),
            ),
          ...upserts.map((row) => this.upsert(row)),
        ]);
        break;
      }
      case "RepoRemoved":
        await db
          .delete(apps)
          .where(and(eq(apps.space, batch.space), eq(apps.repo, batch.did)));
        break;
      case "SpaceDeleted":
        await db.delete(apps).where(eq(apps.space, batch.space));
        break;
    }
  }

  private upsert(row: AppRow) {
    const { uri: _, ...set } = row;
    return this.opts.db
      .insert(apps)
      .values(row)
      .onConflictDoUpdate({ target: apps.uri, set });
  }

  private async write(upserts: AppRow[], deletes: string[]) {
    const { db } = this.opts;
    const statements = [
      ...(deletes.length > 0
        ? [db.delete(apps).where(inArray(apps.uri, deletes))]
        : []),
      ...upserts.map((row) => this.upsert(row)),
    ];
    if (statements.length === 0) return;
    const [first, ...rest] = statements;
    await db.batch([first, ...rest]);
  }
}
