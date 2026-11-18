import {
  customType,
  integer,
  sqliteTable,
  text,
} from "drizzle-orm/sqlite-core";

// The tables behind DrizzleStateStore and DrizzleSessionStore. An app
// re-exports them from its own drizzle schema so drizzle-kit generates their
// migration alongside the app's other tables.

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

// oauthStates holds in-flight authorization requests, keyed by the OAuth
// `state` parameter. Rows are short-lived; expired ones are swept whenever a
// new one is written (see DrizzleStateStore).
export const oauthStates = sqliteTable("oauth_states", {
  key: text("key").primaryKey(),
  value: bytes("value").notNull(),
  expiresAt: integer("expires_at").notNull(),
});

// oauthSessions holds one session per signed-in DID. `value` is the session's
// JSON (token set plus the DPoP private key), sealed with the app's
// credentials key (see seal.ts) since it's a bearer credential for that DID.
export const oauthSessions = sqliteTable("oauth_sessions", {
  did: text("did").primaryKey(),
  value: bytes("value").notNull(),
  updatedAt: integer("updated_at").notNull(),
});
