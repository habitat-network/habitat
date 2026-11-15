import { columnExpression, type HabitatApp } from "./definition.js";
import type { SqlDatabase } from "./sql.js";

/**
 * The slice's tables. One generic records table holds every synced record
 * as JSON, so the sync path never depends on an app's schema. The ACL
 * tables are the materialized answer to "which spaces can this member
 * read", built from the opensocial records that arrive on the same sync
 * stream (see sink.ts).
 */
export const BASE_DDL: readonly string[] = [
  `CREATE TABLE IF NOT EXISTS hab_records (
    uri TEXT PRIMARY KEY,
    cid TEXT NOT NULL,
    space TEXT NOT NULL,
    repo TEXT NOT NULL,
    collection TEXT NOT NULL,
    rkey TEXT NOT NULL,
    value TEXT NOT NULL
  )`,
  `CREATE INDEX IF NOT EXISTS hab_records_space_repo ON hab_records (space, repo)`,
  `CREATE INDEX IF NOT EXISTS hab_records_collection_space ON hab_records (collection, space)`,
  // The roles that may read (and write) each space: the space's
  // community.opensocial.access record. org is the space authority.
  `CREATE TABLE IF NOT EXISTS hab_space_roles (
    space TEXT NOT NULL,
    org TEXT NOT NULL,
    role TEXT NOT NULL,
    PRIMARY KEY (space, role)
  )`,
  `CREATE INDEX IF NOT EXISTS hab_space_roles_org_role ON hab_space_roles (org, role)`,
  // The roles each member holds in each org: the org's
  // community.opensocial.membership records.
  `CREATE TABLE IF NOT EXISTS hab_member_roles (
    org TEXT NOT NULL,
    did TEXT NOT NULL,
    role TEXT NOT NULL,
    PRIMARY KEY (org, did, role)
  )`,
  `CREATE INDEX IF NOT EXISTS hab_member_roles_did ON hab_member_roles (did)`,
  `CREATE TABLE IF NOT EXISTS hab_meta (
    key TEXT PRIMARY KEY,
    value INTEGER NOT NULL
  )`,
  `INSERT OR IGNORE INTO hab_meta (key, value) VALUES ('version', 0)`,
];

const APP_INDEX_PREFIX = "hab_ix_";

export interface AppIndex {
  readonly name: string;
  readonly sql: string;
}

// fnv1a hashes an index's definition into its name, so a changed definition
// is a different index (and the old one is dropped as stale).
function fnv1a(input: string): string {
  let hash = 0x811c9dc5;
  for (let i = 0; i < input.length; i++) {
    hash ^= input.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193);
  }
  return (hash >>> 0).toString(16).padStart(8, "0");
}

/**
 * appIndexes turns the app's declared indexes into partial expression
 * indexes on hab_records. Expression indexes are used rather than generated
 * columns because every collection shares the one table.
 */
export function appIndexes(app: HabitatApp): AppIndex[] {
  const out: AppIndex[] = [];
  for (const [collection, def] of Object.entries(app.definition.collections)) {
    for (const index of def.indexes ?? []) {
      const columns = index
        .map((column) => columnExpression(app, collection, column))
        .join(", ");
      // Collections are validated NSIDs, so inlining one is safe.
      const body = `ON hab_records (${columns}) WHERE collection = '${collection}'`;
      const name = APP_INDEX_PREFIX + fnv1a(body);
      out.push({ name, sql: `CREATE INDEX IF NOT EXISTS ${name} ${body}` });
    }
  }
  return out;
}

/**
 * migrate brings a database up to the app's definition: creates the slice's
 * tables, builds the app's declared indexes, and drops indexes the
 * definition no longer declares. It never touches record data, and is safe
 * to run on every start.
 */
export async function migrate(db: SqlDatabase, app: HabitatApp): Promise<void> {
  await db.batch(BASE_DDL.map((sql) => db.prepare(sql)));
  const wanted = appIndexes(app);
  const { results } = await db
    .prepare(
      `SELECT name FROM sqlite_master WHERE type = 'index' AND name LIKE '${APP_INDEX_PREFIX}%'`,
    )
    .all<{ name: string }>();
  const existing = new Set(results.map((r) => r.name));
  const wantedNames = new Set(wanted.map((i) => i.name));
  const statements = [
    ...[...existing]
      .filter((name) => !wantedNames.has(name))
      .map((name) => db.prepare(`DROP INDEX IF EXISTS ${name}`)),
    ...wanted
      .filter((index) => !existing.has(index.name))
      .map((index) => db.prepare(index.sql)),
  ];
  if (statements.length > 0) await db.batch(statements);
}
