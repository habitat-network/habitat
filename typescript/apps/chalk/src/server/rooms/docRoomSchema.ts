import { sqliteTable, text, integer, blob } from "drizzle-orm/sqlite-core";

// One row, id = 0: this room's merged CRDT snapshot and its identity. A DO
// cannot read back the name it was addressed by, so spaceUri/ownerDid are
// recorded here on first sight and reused for alarm-driven work, when no
// caller is present to supply them.
export const docState = sqliteTable("doc_state", {
  id: integer("id").primaryKey(),
  spaceUri: text("space_uri").notNull(),
  ownerDid: text("owner_did"),
  state: blob("state", { mode: "buffer" }),
  updatedAt: integer("updated_at").notNull(),
});
