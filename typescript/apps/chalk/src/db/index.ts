import { drizzle } from "drizzle-orm/d1";
import { desc, eq } from "drizzle-orm";
import { docs } from "./schema";

export interface DocSummary {
  docId: string;
  uri: string;
  ownerDid: string;
  title: string;
}

export function getDb(env: { DB: D1Database }) {
  return drizzle(env.DB, { schema: { docs } });
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

export async function docsForOwner(
  db: Db,
  ownerDid: string,
): Promise<DocSummary[]> {
  const rows = await db
    .select()
    .from(docs)
    .where(eq(docs.ownerDid, ownerDid))
    .orderBy(desc(docs.updatedAt));
  return rows.map(toSummary);
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
