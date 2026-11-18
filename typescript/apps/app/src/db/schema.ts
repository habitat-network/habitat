// The app's D1 schema: what Chalk (docs, comments, sharing) and Drop
// (files) each kept in their own database, plus the OAuth and space-sync
// tables every Worker app shares.

import {
  customType,
  sqliteTable,
  text,
  integer,
  index,
  primaryKey,
} from "drizzle-orm/sqlite-core";

// bytes is a BLOB column read back as a plain Uint8Array. drizzle's own
// blob({ mode: "buffer" }) hands back a Node Buffer instead, which TanStack
// Start's server-function serializer (seroval) doesn't accept, and D1
// itself returns a blob as an ArrayBuffer or array of numbers depending on
// the query path — new Uint8Array accepts either.
const bytes = customType<{
  data: Uint8Array;
  driverData: ArrayBuffer | number[];
}>({
  dataType: () => "blob",
  fromDriver: (value) => new Uint8Array(value),
});

export const docs = sqliteTable(
  "docs",
  {
    spaceUri: text("space_uri").primaryKey(),
    docId: text("doc_id").notNull().unique(),
    ownerDid: text("owner_did").notNull(),
    title: text("title").notNull(),
    updatedAt: integer("updated_at").notNull(),
  },
  (t) => [index("docs_owner_updated").on(t.ownerDid, t.updatedAt)],
);

// docAccess mirrors network.habitat.relationship.userRelation records synced
// from the doc spaces (see sink.ts's handleUserRelation): one row per live
// grant, keyed by (subjectDid, spaceUri) since a user holds at most one role
// on a given space. The relation record's own URI is kept as a plain column
// (indexed, not the key) purely so a delete tombstone — which carries only
// that URI, not the subject — can still look up which row to remove. Every
// role implies at least reader, so a row's mere presence is "this subject
// can see this doc" — the relation column isn't otherwise consulted yet.
export const docAccess = sqliteTable(
  "doc_access",
  {
    subjectDid: text("subject_did").notNull(),
    spaceUri: text("space_uri").notNull(),
    uri: text("uri").notNull(),
    relation: text("relation").notNull(),
    updatedAt: integer("updated_at").notNull(),
  },
  (t) => [
    primaryKey({ columns: [t.subjectDid, t.spaceUri] }),
    index("doc_access_uri").on(t.uri),
  ],
);

// docOrgAccess mirrors the network.habitat.relationship.spaceRelation
// records that grant a whole org access to a doc — the ones whose subject is
// that org's own community.opensocial.members space (see orgMembersSpaceUri),
// which is how "share with everyone at <org>" is expressed. One row per
// (orgDid, spaceUri): an org holds at most one relation on a given doc.
// Like doc_access, the relation record's own URI is kept as an indexed
// column rather than the key, so a delete tombstone — which carries only
// that URI, not the subject — can still find the row to remove.
export const docOrgAccess = sqliteTable(
  "doc_org_access",
  {
    orgDid: text("org_did").notNull(),
    spaceUri: text("space_uri").notNull(),
    uri: text("uri").notNull(),
    relation: text("relation").notNull(),
    updatedAt: integer("updated_at").notNull(),
  },
  (t) => [
    primaryKey({ columns: [t.orgDid, t.spaceUri] }),
    index("doc_org_access_uri").on(t.uri),
  ],
);

// comments holds a doc's comment *threads* — one row per root
// network.habitat.docs.comment record, which is the only record that
// carries an anchor (a pair of encoded Yjs relative positions, per that
// lexicon's comment) into the doc's CRDT state; replies don't repeat it,
// they just point back at the root by strongRef (see commentReplies
// below). Comments live in their own table rather than alongside docs
// because they're their own space's records: each doc has a companion
// comments space (type "network.habitat.docs.comments", same owner and
// space key as the doc — see commentsSpaceUri in src/server/comments.ts),
// whose readers/writers are inherited from the doc space via
// spaceRelation records.
//
// Keyed by the record's own AT-URI, which is what the syncer delivers on
// both a write and a delete. cid is stored alongside so a reply
// can build the com.atproto.repo.strongRef it needs to reference this
// comment without an extra read. docSpaceUri (not the
// comments space's URI) is stored so listing a doc's comments is a single
// indexed lookup keyed by the same docId the rest of chalk passes around.
export const comments = sqliteTable(
  "comments",
  {
    uri: text("uri").primaryKey(),
    cid: text("cid").notNull(),
    docSpaceUri: text("doc_space_uri").notNull(),
    authorDid: text("author_did").notNull(),
    body: text("body").notNull(),
    anchorStart: bytes("anchor_start").notNull(),
    anchorEnd: bytes("anchor_end").notNull(),
    quotedText: text("quoted_text"),
    createdAt: integer("created_at").notNull(),
  },
  (t) => [index("comments_doc_created").on(t.docSpaceUri, t.createdAt)],
);

// commentReplies mirrors network.habitat.docs.commentReply records — a
// reply within a thread, referencing its root comment record by strongRef
// (commentUri/commentCid) rather than carrying its own anchor. Keyed by
// the reply's own URI, same reasoning as comments above.
export const commentReplies = sqliteTable(
  "comment_replies",
  {
    uri: text("uri").primaryKey(),
    docSpaceUri: text("doc_space_uri").notNull(),
    commentUri: text("comment_uri").notNull(),
    authorDid: text("author_did").notNull(),
    body: text("body").notNull(),
    createdAt: integer("created_at").notNull(),
  },
  (t) => [index("comment_replies_thread").on(t.docSpaceUri, t.commentUri)],
);

// --- Orgs --------------------------------------------------------------------

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

// --- File index (the syncer's SyncSink, src/server/sink.ts) -----------------

// files is the synced view of every network.habitat.drop.file record, which
// is what the file list reads. Keyed by the record's own URI; `cid` is the
// record's CID so a replayed batch (delivery is at-least-once) is a no-op
// update. orgDid is the space authority — each file is its own org space.
// blobCid is the CID of the file's blob in its space (see api.download.ts).
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

// --- OAuth (src/server/oauth.ts) -------------------------------------------
//
// The app is its own atproto OAuth client of the Habitat instance. It holds
// sessions for members (everything done on a member's behalf) and for orgs a
// member has connected (creating an org's doc and file spaces, managing their
// sharing, minting their delegation tokens). Both live in oauth_sessions
// keyed by DID. `value` is the session's JSON (token set plus the DPoP private
// key), sealed with APP_CREDENTIALS_KEY (internal/oauthClient/seal.ts), since
// it's a bearer credential for that DID.
export { oauthSessions, oauthStates } from "internal/oauthClient/schema";

// --- Space sync (internal/spaceSync/drizzle's SyncStore) ----------------------

// The tables live with DrizzleSyncStore, shared with the other apps; they are
// re-exported here so drizzle-kit migrates them with this schema.
export { syncRepos, syncSpaces } from "internal/spaceSync/drizzle";
