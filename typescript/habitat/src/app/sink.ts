import type { HabitatApp } from "./definition.js";
import { runBatch, type SqlDatabase, type SqlStatement } from "./sql.js";

/** The opensocial records the slice's ACL tables are built from. */
export const ACCESS_COLLECTION = "community.opensocial.access";
export const MEMBERSHIP_COLLECTION = "community.opensocial.membership";
export const MEMBERS_SPACE_TYPE = "community.opensocial.members";

interface CidLike {
  toString(): string;
}

/**
 * A verified batch of changes to one repo in one space. Structurally the
 * batches a space syncer hands its sink (internal/spaceSync's
 * PromiseRepoBatch), declared here so the SDK doesn't depend on the syncer.
 */
export type SliceBatch =
  | {
      readonly _tag: "Ops";
      readonly space: string;
      readonly did: string;
      readonly changes: ReadonlyArray<{
        readonly uri: string;
        readonly collection: string;
        readonly rkey: string;
        /** null is a delete. */
        readonly cid: CidLike | null;
        readonly value?: unknown;
      }>;
    }
  | {
      /** Replace the repo's contents in the space with exactly these records. */
      readonly _tag: "Reset";
      readonly space: string;
      readonly did: string;
      readonly records: AsyncIterable<{
        readonly collection: string;
        readonly rkey: string;
        readonly cid: CidLike;
        readonly record: unknown;
      }>;
    }
  | {
      readonly _tag: "RepoRemoved";
      readonly space: string;
      readonly did: string;
    }
  | { readonly _tag: "SpaceDeleted"; readonly space: string };

export interface ParsedSpace {
  readonly authority: string;
  readonly type: string;
  readonly key: string;
}

/** parseSpace splits a space reference, `at://{authority}/space/{type}/{key}`. */
export function parseSpace(space: string): ParsedSpace | undefined {
  const match = /^at:\/\/([^/]+)\/space\/([^/]+)\/([^/]+)$/.exec(space);
  if (!match) return undefined;
  return { authority: match[1]!, type: match[2]!, key: match[3]! };
}

/**
 * toLexJson converts a decoded record value to its JSON form: CIDs become
 * `{"$link": …}` and byte arrays `{"$bytes": …}`, as in atproto's JSON
 * encoding, so stored values can be queried with SQLite's JSON functions.
 */
export function toLexJson(value: unknown): unknown {
  if (value === null || typeof value !== "object") return value;
  if (value instanceof Uint8Array) {
    let binary = "";
    for (const byte of value) binary += String.fromCharCode(byte);
    return { $bytes: btoa(binary).replace(/=+$/, "") };
  }
  if (Array.isArray(value)) return value.map(toLexJson);
  const object = value as Record<string, unknown>;
  // A CID (multiformats' CID class, which @atproto/lex decodes links to).
  if ("multihash" in object && "code" in object && "version" in object) {
    return { $link: String(value) };
  }
  const out: Record<string, unknown> = {};
  for (const [key, child] of Object.entries(object)) {
    out[key] = toLexJson(child);
  }
  return out;
}

function stringArray(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((v): v is string => typeof v === "string")
    : [];
}

/**
 * SliceSink applies verified sync batches to the slice. It is the only
 * writer of the slice besides a mutation's write-through (runtime.ts).
 *
 * Records in the app's declared collections are stored as JSON, whatever
 * their shape. The opensocial access and membership records are not stored
 * as records: they are folded into the ACL tables, and only when authored
 * by the space's authority, since a member's own repo in the same space
 * could otherwise grant itself access.
 *
 * Each batch is one transaction, so a failed apply leaves the slice as it
 * was. Delivery is at-least-once; every write is an upsert or a delete, so
 * a replay is a no-op.
 */
export class SliceSink {
  private readonly collections: ReadonlySet<string>;

  constructor(
    private readonly db: SqlDatabase,
    app: HabitatApp,
  ) {
    this.collections = new Set(Object.keys(app.definition.collections));
  }

  async apply(batch: SliceBatch, signal?: AbortSignal): Promise<void> {
    const statements: SqlStatement[] = [];
    switch (batch._tag) {
      case "Ops":
        for (const change of batch.changes) {
          if (change.cid === null) {
            statements.push(
              ...this.remove(
                batch.space,
                batch.did,
                change.collection,
                change.rkey,
                change.uri,
              ),
            );
          } else if (change.value !== undefined) {
            statements.push(
              ...this.put(
                batch.space,
                batch.did,
                change.collection,
                change.rkey,
                change.uri,
                change.cid.toString(),
                change.value,
              ),
            );
          }
        }
        break;
      case "Reset": {
        // Drain every record first: draining is what verifies them, and a
        // verification failure must fail the apply before anything changes.
        const puts: SqlStatement[] = [];
        for await (const record of batch.records) {
          const uri = `${batch.space}/${batch.did}/${record.collection}/${record.rkey}`;
          puts.push(
            ...this.put(
              batch.space,
              batch.did,
              record.collection,
              record.rkey,
              uri,
              record.cid.toString(),
              record.record,
            ),
          );
        }
        statements.push(...this.clearRepo(batch.space, batch.did), ...puts);
        break;
      }
      case "RepoRemoved":
        statements.push(...this.clearRepo(batch.space, batch.did));
        break;
      case "SpaceDeleted": {
        const { db } = this;
        statements.push(
          db
            .prepare(`DELETE FROM hab_records WHERE space = ?`)
            .bind(batch.space),
          db
            .prepare(`DELETE FROM hab_space_roles WHERE space = ?`)
            .bind(batch.space),
        );
        const parsed = parseSpace(batch.space);
        if (parsed?.type === MEMBERS_SPACE_TYPE) {
          statements.push(
            db
              .prepare(`DELETE FROM hab_member_roles WHERE org = ?`)
              .bind(parsed.authority),
          );
        }
        break;
      }
    }
    if (statements.length === 0) return;
    signal?.throwIfAborted();
    statements.push(bumpVersion(this.db));
    await runBatch(this.db, statements);
  }

  // clearRepo removes everything one repo contributed to a space: its
  // records and, when the repo is the space authority's, the ACL rows its
  // records defined.
  private clearRepo(space: string, did: string): SqlStatement[] {
    const { db } = this;
    const statements = [
      db
        .prepare(`DELETE FROM hab_records WHERE space = ? AND repo = ?`)
        .bind(space, did),
    ];
    const parsed = parseSpace(space);
    if (parsed?.authority === did) {
      statements.push(
        db.prepare(`DELETE FROM hab_space_roles WHERE space = ?`).bind(space),
      );
      if (parsed.type === MEMBERS_SPACE_TYPE) {
        statements.push(
          db.prepare(`DELETE FROM hab_member_roles WHERE org = ?`).bind(did),
        );
      }
    }
    return statements;
  }

  private put(
    space: string,
    did: string,
    collection: string,
    rkey: string,
    uri: string,
    cid: string,
    value: unknown,
  ): SqlStatement[] {
    const { db } = this;
    const parsed = parseSpace(space);
    const json = toLexJson(value) as Record<string, unknown> | null;
    if (collection === ACCESS_COLLECTION) {
      if (parsed?.authority !== did || rkey !== "self") return [];
      return [
        db.prepare(`DELETE FROM hab_space_roles WHERE space = ?`).bind(space),
        ...stringArray(json?.roles).map((role) =>
          db
            .prepare(
              `INSERT OR IGNORE INTO hab_space_roles (space, org, role) VALUES (?, ?, ?)`,
            )
            .bind(space, did, role),
        ),
      ];
    }
    if (collection === MEMBERSHIP_COLLECTION) {
      if (parsed?.authority !== did || parsed.type !== MEMBERS_SPACE_TYPE)
        return [];
      // The record key is the member's DID.
      return [
        db
          .prepare(`DELETE FROM hab_member_roles WHERE org = ? AND did = ?`)
          .bind(did, rkey),
        ...stringArray(json?.roles).map((role) =>
          db
            .prepare(
              `INSERT OR IGNORE INTO hab_member_roles (org, did, role) VALUES (?, ?, ?)`,
            )
            .bind(did, rkey, role),
        ),
      ];
    }
    if (!this.collections.has(collection)) return [];
    return [
      upsertRecord(db, {
        uri,
        cid,
        space,
        repo: did,
        collection,
        rkey,
        value: json,
      }),
    ];
  }

  private remove(
    space: string,
    did: string,
    collection: string,
    rkey: string,
    uri: string,
  ): SqlStatement[] {
    const { db } = this;
    const parsed = parseSpace(space);
    if (collection === ACCESS_COLLECTION) {
      if (parsed?.authority !== did || rkey !== "self") return [];
      return [
        db.prepare(`DELETE FROM hab_space_roles WHERE space = ?`).bind(space),
      ];
    }
    if (collection === MEMBERSHIP_COLLECTION) {
      if (parsed?.authority !== did || parsed.type !== MEMBERS_SPACE_TYPE)
        return [];
      return [
        db
          .prepare(`DELETE FROM hab_member_roles WHERE org = ? AND did = ?`)
          .bind(did, rkey),
      ];
    }
    if (!this.collections.has(collection)) return [];
    return [db.prepare(`DELETE FROM hab_records WHERE uri = ?`).bind(uri)];
  }
}

export interface RecordRow {
  uri: string;
  cid: string;
  space: string;
  repo: string;
  collection: string;
  rkey: string;
  value: unknown;
}

export function upsertRecord(db: SqlDatabase, row: RecordRow): SqlStatement {
  return db
    .prepare(
      `INSERT INTO hab_records (uri, cid, space, repo, collection, rkey, value)
       VALUES (?, ?, ?, ?, ?, ?, ?)
       ON CONFLICT (uri) DO UPDATE SET cid = excluded.cid, value = excluded.value`,
    )
    .bind(
      row.uri,
      row.cid,
      row.space,
      row.repo,
      row.collection,
      row.rkey,
      JSON.stringify(row.value),
    );
}

/**
 * bumpVersion advances the slice's version counter. It moves with every
 * committed change, so a client can poll it cheaply and refetch its queries
 * only when it has moved.
 */
export function bumpVersion(db: SqlDatabase): SqlStatement {
  return db.prepare(
    `UPDATE hab_meta SET value = value + 1 WHERE key = 'version'`,
  );
}
