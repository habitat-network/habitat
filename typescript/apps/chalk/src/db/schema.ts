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
