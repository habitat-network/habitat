import {
  sqliteTable,
  text,
  integer,
  index,
  primaryKey,
} from "drizzle-orm/sqlite-core";

export const docs = sqliteTable(
  "docs",
  {
    spaceUri: text("space_uri").primaryKey(),
    docId: text("doc_id").notNull().unique(),
    ownerDid: text("owner_did").notNull(),
    title: text("title").notNull(),
    updatedAt: integer("updated_at").notNull(),
    isOrg: integer("is_org", { mode: "boolean" }).notNull().default(false),
  },
  (t) => [index("docs_owner_updated").on(t.ownerDid, t.updatedAt)],
);

// docAccess mirrors network.habitat.relationship.userRelation records synced
// from the outbox (see sapChannel.ts's handleOutboxMessage): one row per live
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

// connectedOrgs records that a member has successfully connected an org
// (see session.org-callback.tsx) — chalk's own audit trail, separate from
// the live "orgs I'm a member of" list, which is always fetched fresh from
// pear rather than cached here.
export const connectedOrgs = sqliteTable(
  "connected_orgs",
  {
    memberDid: text("member_did").notNull(),
    orgDid: text("org_did").notNull(),
    orgName: text("org_name").notNull(),
    connectedAt: integer("connected_at").notNull(),
  },
  (t) => [primaryKey({ columns: [t.memberDid, t.orgDid] })],
);

// comments holds a doc's comment *threads* — one row per root
// network.habitat.docs.comment record, which is the only record that
// carries an anchor (a pair of base64 Yjs relative positions, per that
// lexicon's comment) into the doc's CRDT state; replies don't repeat it,
// they just point back at the root by strongRef (see commentReplies
// below). Comments live in their own table rather than alongside docs
// because they're their own space's records: each doc has a companion
// comments space (type "network.habitat.docs.comments", same owner and
// space key as the doc — see commentsSpaceUri in src/server/comments.ts),
// whose readers/writers are inherited from the doc space via
// spaceRelation records.
//
// Keyed by the record's own AT-URI, which is what the outbox delivers on
// both a write and a delete tombstone. cid is stored alongside so a reply
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
    anchorStart: text("anchor_start").notNull(),
    anchorEnd: text("anchor_end").notNull(),
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
