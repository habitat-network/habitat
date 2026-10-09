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
// Drop is its own atproto OAuth client: it holds sessions both for members
// (to know who is signed in and which orgs they belong to) and for orgs
// themselves (the org's own session creates file spaces, writes file
// records, and mints the delegation tokens the syncer exchanges for space
// credentials). Both live in the same table, keyed by DID — an org is just
// another OAuth subject. `value` is the session's JSON (token set plus the
// DPoP private key), sealed with DROP_CREDENTIALS_KEY (src/server/seal.ts),
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

// --- Space sync (internal/spaceSync's SyncStore, src/db/syncStore.ts) -------

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

// --- File index (the syncer's SyncSink, src/server/sink.ts) -----------------

// files is the synced view of every network.habitat.drop.file record, which
// is what the file list reads. Keyed by the record's own URI; `cid` is the
// record's CID so a replayed batch (delivery is at-least-once) is a no-op
// update. orgDid is the space authority — each file is its own org space.
// blobCid names the R2 object holding the file's bytes (see sink.ts).
export const files = sqliteTable(
  "files",
  {
    uri: text("uri").primaryKey(),
    cid: text("cid").notNull(),
    space: text("space").notNull(),
    repo: text("repo").notNull(),
    orgDid: text("org_did").notNull(),
    name: text("name").notNull(),
    blobCid: text("blob_cid").notNull(),
    mimeType: text("mime_type").notNull(),
    size: integer("size").notNull(),
    uploadedBy: text("uploaded_by"),
    createdAt: integer("created_at").notNull(),
  },
  (t) => [
    index("files_org_created").on(t.orgDid, t.createdAt),
    index("files_space_repo").on(t.space, t.repo),
  ],
);
