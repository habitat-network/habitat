import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { drizzle } from "drizzle-orm/better-sqlite3";
import { migrate } from "drizzle-orm/better-sqlite3/migrator";
import * as schema from "./schema";

// better-sqlite3 doesn't create intermediate directories for a file-backed
// database, unlike node:sqlite's DatabaseSync used to (implicitly, via a
// pre-existing directory). Skip for the in-memory/temp special values used
// by tests.
const databaseUrl = process.env.DATABASE_URL ?? ".chalk/chalk.db";
if (databaseUrl !== ":memory:") {
  fs.mkdirSync(path.dirname(databaseUrl), { recursive: true });
}

export const db = drizzle(databaseUrl, { schema });

// Apply the committed migrations (see ../../drizzle) on every boot — this is
// the module-level composition root's db singleton, constructed once at
// import time, so there's no separate deploy step to run `drizzle-kit
// migrate` from; doing it here keeps a fresh or upgraded sqlite file always
// in sync with the schema the running code expects.
migrate(db, {
  migrationsFolder: path.join(
    path.dirname(fileURLToPath(import.meta.url)),
    "..",
    "..",
    "drizzle",
  ),
});
