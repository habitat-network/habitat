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
