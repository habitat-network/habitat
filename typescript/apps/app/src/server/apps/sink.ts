import { eq } from "drizzle-orm";
import { getBlobCidString, getBlobSize, type LexMap } from "@atproto/lex";

import { AtUri, SpaceRef } from "@atproto/syntax";
import { network } from "api";
import { Effect } from "effect";
import type { Domain } from "../stream/domain";
import type { Db } from "@/db";
import { apps } from "@/db/schema";
import { hostLabel } from "@/lib/apps";

// Records written before the spaces alpha moved space URIs to the at:space/
// form still embed the older at://{did}/space/... form, which only lenient
// validation accepts. Goes with the migration to the new form.
const LENIENT = { strict: false };

export const APP_COLLECTION = network.habitat.apps.app.$nsid;

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
  const parsed = network.habitat.apps.app.$safeParse(value, LENIENT);
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

// appsDomain keeps the apps table equal to the set of
// network.habitat.apps.app records across every synced app space. The code
// isn't copied: the runner reads it from the group's space host when an
// app's isolate starts (see runner.ts). Upserts by uri, so a replay is a
// no-op.
export function appsDomain(db: Db): Domain {
  return {
    collections: [
      {
        collection: APP_COLLECTION,
        handlers: {
          put: (event) =>
            Effect.tryPromise(async () => {
              const row = toAppRow(event.uri, event.cid, event.record);
              // A record that no longer parses takes the app down rather than
              // leaving its last good version running.
              if (!row) {
                await db.delete(apps).where(eq(apps.uri, event.uri));
                return;
              }
              const { uri: _, ...set } = row;
              await db
                .insert(apps)
                .values(row)
                .onConflictDoUpdate({ target: apps.uri, set });
            }),
          del: (event) =>
            Effect.tryPromise(async () => {
              await db.delete(apps).where(eq(apps.uri, event.uri));
            }),
        },
      },
    ],
    spaceDeleted: (event) =>
      Effect.tryPromise(async () => {
        await db.delete(apps).where(eq(apps.space, event.space));
      }),
  };
}
