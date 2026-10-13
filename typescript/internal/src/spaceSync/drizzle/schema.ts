import {
  customType,
  index,
  integer,
  primaryKey,
  sqliteTable,
  text,
} from "drizzle-orm/sqlite-core";

// The tables behind DrizzleSyncStore. An app re-exports them from its own
// drizzle schema so drizzle-kit generates their migration alongside the
// app's other tables.

// bytes is a BLOB column read back as a plain Uint8Array: D1 hands a blob
// back as an ArrayBuffer or an array of numbers depending on the query path,
// and new Uint8Array accepts either.
const bytes = customType<{
  data: Uint8Array;
  driverData: ArrayBuffer | number[];
}>({
  dataType: () => "blob",
  fromDriver: (value) => new Uint8Array(value),
});

// syncSpaces is SpaceState: one row per watched space. Times are epoch ms.
export const syncSpaces = sqliteTable(
  "sync_spaces",
  {
    space: text("space").primaryKey(),
    authority: text("authority").notNull(),
    spaceRev: text("space_rev"),
    registrationExpiresAt: integer("registration_expires_at"),
    nextDueAt: integer("next_due_at").notNull(),
    lastFullPassAt: integer("last_full_pass_at"),
    failures: integer("failures").notNull(),
    lastError: text("last_error"),
  },
  (t) => [index("sync_spaces_due").on(t.nextDueAt)],
);

// syncRepos is RepoState: the last verified revision of each writer's repo
// in a space, plus its 2048-byte LtHash state.
export const syncRepos = sqliteTable(
  "sync_repos",
  {
    space: text("space").notNull(),
    did: text("did").notNull(),
    rev: text("rev").notNull(),
    ltHash: bytes("lt_hash").notNull(),
  },
  (t) => [primaryKey({ columns: [t.space, t.did] })],
);
