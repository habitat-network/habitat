import {
  customType,
  sqliteTable,
  text,
  integer,
  index,
  primaryKey,
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
// session per signed-in member: the member's own session lists their orgs and
// searches as them. `value` is the session's JSON
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

// --- Chats ----------------------------------------------------------------

// chats is one row per chat, owned by the signed-in member who started it.
// `id` is a UUID the browser picks when the member starts a new chat; the row
// is created with the chat's first message, whose text names it.
export const chats = sqliteTable(
  "chats",
  {
    id: text("id").primaryKey(),
    ownerDid: text("owner_did").notNull(),
    title: text("title").notNull(),
    createdAt: integer("created_at").notNull(),
  },
  (t) => [index("chats_owner_created").on(t.ownerDid, t.createdAt)],
);

// messages holds the text of each message of a chat. The id is the one the
// chat UI knows the message by, so it is only unique within its chat.
export const messages = sqliteTable(
  "messages",
  {
    chatId: text("chat_id")
      .notNull()
      .references(() => chats.id, { onDelete: "cascade" }),
    id: text("id").notNull(),
    role: text("role").notNull(),
    content: text("content").notNull(),
    createdAt: integer("created_at").notNull(),
  },
  (t) => [
    primaryKey({ columns: [t.chatId, t.id] }),
    index("messages_chat_created").on(t.chatId, t.createdAt),
  ],
);
