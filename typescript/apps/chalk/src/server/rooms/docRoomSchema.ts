import { sqliteTable, text, integer, blob, primaryKey } from "drizzle-orm/sqlite-core";

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

export const pendingFlush = sqliteTable(
  "pending_flush",
  {
    // kind is "member" or "owner"; memberDid is "" for the owner flush, so
    // (kind, memberDid) is a usable composite key.
    kind: text("kind").notNull(),
    memberDid: text("member_did").notNull(),
    firstPushAt: integer("first_push_at").notNull(),
    idleDeadline: integer("idle_deadline").notNull(),
  },
  (t) => [primaryKey({ columns: [t.kind, t.memberDid] })],
);
