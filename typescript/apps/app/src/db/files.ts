// Queries over the synced files table and connected orgs, ported from Drop.
import { desc, eq, inArray, sql } from "drizzle-orm";
import { connectedOrgs, files } from "./schema";
import type { Db } from ".";

export interface FileSummary {
  uri: string;
  space: string;
  name: string;
  mimeType: string;
  size: number;
  uploadedBy: string | null;
  createdAt: number;
}

// filesForOrg lists an org's synced files, newest first. Only what the file
// list renders — the blob CID stays server-side, behind the download route's
// membership check.
export async function filesForOrg(
  db: Db,
  orgDid: string,
): Promise<FileSummary[]> {
  return db
    .select({
      uri: files.uri,
      space: files.space,
      name: files.name,
      mimeType: files.mimeType,
      size: files.size,
      uploadedBy: files.uploadedBy,
      createdAt: files.createdAt,
    })
    .from(files)
    .where(eq(files.orgDid, orgDid))
    .orderBy(desc(files.createdAt), desc(files.uri));
}

export async function fileByUri(db: Db, uri: string) {
  const [row] = await db
    .select()
    .from(files)
    .where(eq(files.uri, uri))
    .limit(1);
  return row;
}

// upsertConnectedOrg records that this deployment now holds the org's own
// OAuth session (see the org connect flow in src/routes/oauth.callback.ts).
export async function upsertConnectedOrg(
  db: Db,
  org: { orgDid: string; name: string; connectedBy: string },
): Promise<void> {
  const row = { ...org, connectedAt: Date.now() };
  await db
    .insert(connectedOrgs)
    .values(row)
    .onConflictDoUpdate({
      target: connectedOrgs.orgDid,
      set: {
        name: row.name,
        connectedBy: row.connectedBy,
        connectedAt: row.connectedAt,
      },
    });
}

export async function deleteConnectedOrg(db: Db, orgDid: string) {
  await db.delete(connectedOrgs).where(eq(connectedOrgs.orgDid, orgDid));
}

// orgStreamCursor reads the seq org's event stream resumes from.
export async function orgStreamCursor(
  db: Db,
  orgDid: string,
): Promise<number | undefined> {
  const [row] = await db
    .select({ cursor: connectedOrgs.streamCursor })
    .from(connectedOrgs)
    .where(eq(connectedOrgs.orgDid, orgDid))
    .limit(1);
  return row?.cursor ?? undefined;
}

// setOrgStreamCursor saves org's stream cursor. It only moves forward: more
// than one isolate may be following the same org, and a slower one must not
// move the cursor back.
export async function setOrgStreamCursor(
  db: Db,
  orgDid: string,
  cursor: number,
): Promise<void> {
  await db
    .update(connectedOrgs)
    .set({
      streamCursor: sql`max(coalesce(${connectedOrgs.streamCursor}, 0), ${cursor})`,
    })
    .where(eq(connectedOrgs.orgDid, orgDid));
}

// connectedOrgNames maps each of orgIds this deployment holds a session for
// to its cached name.
export async function connectedOrgNames(
  db: Db,
  orgIds: string[],
): Promise<Map<string, string>> {
  if (orgIds.length === 0) return new Map();
  const rows = await db
    .select({ orgDid: connectedOrgs.orgDid, name: connectedOrgs.name })
    .from(connectedOrgs)
    .where(inArray(connectedOrgs.orgDid, orgIds));
  return new Map(rows.map((r) => [r.orgDid, r.name]));
}

export async function isConnectedOrg(db: Db, orgDid: string) {
  const [row] = await db
    .select({ orgDid: connectedOrgs.orgDid })
    .from(connectedOrgs)
    .where(eq(connectedOrgs.orgDid, orgDid))
    .limit(1);
  return row !== undefined;
}
