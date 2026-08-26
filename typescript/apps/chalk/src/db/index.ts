import { drizzle } from "drizzle-orm/d1";
import { desc, eq } from "drizzle-orm";
import { docs, docAccess } from "./schema";

export interface DocSummary {
  docId: string;
  uri: string;
  ownerDid: string;
  title: string;
}

export function getDb(env: { DB: D1Database }) {
  return drizzle(env.DB, { schema: { docs, docAccess } });
}

export type Db = ReturnType<typeof getDb>;

export async function upsertDoc(
  db: Db,
  doc: { spaceUri: string; docId: string; ownerDid: string; title: string },
): Promise<void> {
  const row = { ...doc, updatedAt: Date.now() };
  await db
    .insert(docs)
    .values(row)
    .onConflictDoUpdate({
      target: docs.spaceUri,
      set: {
        docId: row.docId,
        ownerDid: row.ownerDid,
        title: row.title,
        updatedAt: row.updatedAt,
      },
    });
}

function toSummary(r: typeof docs.$inferSelect): DocSummary {
  return {
    docId: r.docId,
    uri: r.spaceUri,
    ownerDid: r.ownerDid,
    title: r.title,
  };
}

// docsForAccessor returns the docs a subject holds any role on, per the
// doc_access rows synced from network.habitat.relationship.userRelation
// (see sapChannel.ts). The (subjectDid, spaceUri) primary key on doc_access
// means each doc joins in at most once here.
export async function docsForAccessor(
  db: Db,
  subjectDid: string,
): Promise<DocSummary[]> {
  const rows = await db
    .select({
      spaceUri: docs.spaceUri,
      docId: docs.docId,
      ownerDid: docs.ownerDid,
      title: docs.title,
      updatedAt: docs.updatedAt,
    })
    .from(docs)
    .innerJoin(docAccess, eq(docs.spaceUri, docAccess.spaceUri))
    .where(eq(docAccess.subjectDid, subjectDid))
    .orderBy(desc(docs.updatedAt));
  return rows.map(toSummary);
}

// upsertDocAccess records or updates a live grant, keyed by
// (subjectDid, spaceUri) — a user holds at most one role on a given space.
// uri (the relation record's own URI) is stored alongside purely so the
// matching delete tombstone, which carries only that URI, can find this row.
export async function upsertDocAccess(
  db: Db,
  access: {
    uri: string;
    spaceUri: string;
    subjectDid: string;
    relation: string;
  },
): Promise<void> {
  const row = { ...access, updatedAt: Date.now() };
  await db
    .insert(docAccess)
    .values(row)
    .onConflictDoUpdate({
      target: [docAccess.subjectDid, docAccess.spaceUri],
      set: {
        uri: row.uri,
        relation: row.relation,
        updatedAt: row.updatedAt,
      },
    });
}

// deleteDocAccess removes a grant by its relation record's URI, in response
// to the JSON-null tombstone the outbox emits for a deleted record.
export async function deleteDocAccess(db: Db, uri: string): Promise<void> {
  await db.delete(docAccess).where(eq(docAccess.uri, uri));
}

export async function docByUri(
  db: Db,
  spaceUri: string,
): Promise<DocSummary | undefined> {
  const [row] = await db
    .select()
    .from(docs)
    .where(eq(docs.spaceUri, spaceUri))
    .limit(1);
  return row ? toSummary(row) : undefined;
}
