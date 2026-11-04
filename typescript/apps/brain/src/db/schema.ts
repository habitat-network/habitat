import {
  customType,
  sqliteTable,
  text,
  integer,
  index,
} from "drizzle-orm/sqlite-core";

// bytes is a BLOB column read back as a plain Uint8Array — same reasoning as
// chalk's: D1 hands a blob back as an ArrayBuffer or an array of numbers
// depending on the query path, and new Uint8Array accepts either.
const bytes = customType<{
  data: Uint8Array;
  driverData: ArrayBuffer | number[];
}>({
  dataType: () => "blob",
  fromDriver: (value) => new Uint8Array(value),
});

// --- OAuth (src/server/oauth.ts) -------------------------------------------
//
// Brain is its own atproto OAuth client of the Habitat instance. It holds a
// session per signed-in member: the member's own session creates chat spaces,
// writes their messages, searches as them, and mints the delegation tokens
// the syncer exchanges for space credentials. `value` is the session's JSON
// (token set plus the DPoP private key), sealed with BRAIN_CREDENTIALS_KEY
// (src/server/seal.ts), since it's a bearer credential for that DID.

// oauthStates holds in-flight authorization requests, keyed by the OAuth
// `state` parameter. Rows are short-lived; expired ones are swept whenever a
// new one is written (see DrizzleStateStore).
export const oauthStates = sqliteTable("oauth_states", {
  key: text("key").primaryKey(),
  value: bytes("value").notNull(),
  expiresAt: integer("expires_at").notNull(),
});

export const oauthSessions = sqliteTable("oauth_sessions", {
  did: text("did").primaryKey(),
  value: bytes("value").notNull(),
  updatedAt: integer("updated_at").notNull(),
});

// --- Space sync (internal/spaceSync/drizzle's SyncStore) ----------------------

// The tables live with DrizzleSyncStore, shared with the other apps; they are
// re-exported here so drizzle-kit migrates them with this schema.
export { syncRepos, syncSpaces } from "internal/spaceSync/drizzle";

// --- Chat index (the syncer's SyncSink, src/server/sink.ts) -------------------

// chats is the synced view of every network.habitat.brain.session record:
// one row per chat space. ownerDid is the space authority, i.e. the member
// whose chat it is.
export const chats = sqliteTable(
  "chats",
  {
    space: text("space").primaryKey(),
    cid: text("cid").notNull(),
    ownerDid: text("owner_did").notNull(),
    title: text("title").notNull(),
    createdAt: integer("created_at").notNull(),
  },
  (t) => [index("chats_owner_created").on(t.ownerDid, t.createdAt)],
);

// messages is the synced view of every network.habitat.brain.message record,
// keyed by the record's own URI. `cid` is the record's CID so a replayed batch
// (delivery is at-least-once) is a no-op update.
export const messages = sqliteTable(
  "messages",
  {
    uri: text("uri").primaryKey(),
    cid: text("cid").notNull(),
    space: text("space").notNull(),
    repo: text("repo").notNull(),
    role: text("role").notNull(),
    content: text("content").notNull(),
    createdAt: integer("created_at").notNull(),
  },
  (t) => [
    index("messages_space_created").on(t.space, t.createdAt),
    index("messages_space_repo").on(t.space, t.repo),
  ],
);
