import {
  customType,
  sqliteTable,
  text,
  integer,
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
// Chat is its own atproto OAuth client: it holds sessions both for members
// (to know who is signed in and which orgs they belong to) and for orgs
// themselves (the org's own session creates file spaces, writes file
// records, and mints the delegation tokens the syncer exchanges for space
// credentials). Both live in the same table, keyed by DID — an org is just
// another OAuth subject. `value` is the session's JSON (token set plus the
// DPoP private key), sealed with CHAT_CREDENTIALS_KEY (src/server/seal.ts),
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

// connectedOrgs records the orgs this deployment holds an OAuth session for
// — one row per org, not per member: any admin's connection serves every
// member. The name is cached from the org's profile at connect time so the
// org picker doesn't re-read it on every load.
export const connectedOrgs = sqliteTable("connected_orgs", {
  orgDid: text("org_did").primaryKey(),
  name: text("name").notNull(),
  connectedBy: text("connected_by").notNull(),
  connectedAt: integer("connected_at").notNull(),
});

// --- Space sync (internal/spaceSync/drizzle's SyncStore) ----------------------

// The tables live with DrizzleSyncStore, shared with the other apps; they are
// re-exported here so drizzle-kit migrates them with this schema.
export { syncRepos, syncSpaces } from "internal/spaceSync/drizzle";

// --- The Habitat app slice --------------------------------------------------
//
// Chat's synced records and their permissions aren't drizzle tables: they
// live in the hab_* tables the Habitat app runtime creates and indexes
// itself from src/habitat/app.ts (see src/server/runtime.ts).
