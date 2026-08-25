import { sqliteTable, text, integer, index } from "drizzle-orm/sqlite-core";

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
