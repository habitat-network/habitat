import { sqliteTable, text, integer } from "drizzle-orm/sqlite-core";

// docs holds the metadata chalk has discovered for a doc (via its own
// creates or the sap-outbox consumer): the space it lives in, its owner,
// and a display title. Mirrors the previous node:sqlite `docs` table.
export const docs = sqliteTable("docs", {
  spaceUri: text("space_uri").primaryKey(),
  docId: text("doc_id").notNull().unique(),
  ownerDid: text("owner_did").notNull(),
  title: text("title").notNull(),
  updatedAt: integer("updated_at").notNull(),
});

// docCrdt caches each space's merged Yjs CRDT snapshot (base64-encoded
// `Y.encodeStateAsUpdateV2` bytes) so it survives restarts without
// replaying every member delta. Mirrors the previous node:sqlite
// `doc_crdt` table.
export const docCrdt = sqliteTable("doc_crdt", {
  spaceUri: text("space_uri").primaryKey(),
  state: text("state").notNull(),
  updatedAt: integer("updated_at").notNull(),
});
