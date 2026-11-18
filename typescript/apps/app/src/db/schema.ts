import {
  customType,
  sqliteTable,
  text,
  integer,
} from "drizzle-orm/sqlite-core";

// bytes is a BLOB column read back as a plain Uint8Array: D1 hands a blob back
// as an ArrayBuffer or an array of numbers depending on the query path, and
// new Uint8Array accepts either.
const bytes = customType<{
  data: Uint8Array;
  driverData: ArrayBuffer | number[];
}>({
  dataType: () => "blob",
  fromDriver: (value) => new Uint8Array(value),
});

// --- OAuth (src/server/oauth.ts) -------------------------------------------
//
// The app is an atproto OAuth client of the Habitat instance and holds a
// session per signed-in member. `value` is the session's JSON (token set plus
// the DPoP private key), sealed with APP_CREDENTIALS_KEY (src/server/seal.ts)
// since it's a bearer credential for that DID.

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
