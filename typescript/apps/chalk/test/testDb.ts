import Database from "better-sqlite3";
import { drizzle } from "drizzle-orm/better-sqlite3";
import * as schema from "../src/db/schema";

// createTestDb builds an in-memory drizzle+better-sqlite3 instance for
// DocStore tests, with the same tables `drizzle-kit generate`'s migration
// creates for schema.ts — hand-written here rather than running the
// generated migration file so tests don't depend on that file's exact
// name/location, only on staying in sync with schema.ts.
export function createTestDb() {
  const sqlite = new Database(":memory:");
  sqlite.exec(`
    CREATE TABLE docs (
      space_uri  TEXT PRIMARY KEY,
      doc_id     TEXT NOT NULL UNIQUE,
      owner_did  TEXT NOT NULL,
      title      TEXT NOT NULL,
      updated_at INTEGER NOT NULL
    );
    CREATE TABLE doc_crdt (
      space_uri  TEXT PRIMARY KEY,
      state      TEXT NOT NULL,
      updated_at INTEGER NOT NULL
    );
  `);
  return drizzle(sqlite, { schema });
}
