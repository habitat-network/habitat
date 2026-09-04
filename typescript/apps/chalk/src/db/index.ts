import { drizzle } from "drizzle-orm/d1";
import { and, desc, eq, inArray } from "drizzle-orm";
import { docs, docAccess, connectedOrgs } from "./schema";

export interface DocSummary {
  docId: string;
  uri: string;
  ownerDid: string;
  title: string;
  isOrg: boolean;
}

export function getDb(env: { DB: D1Database }) {
  return drizzle(env.DB, { schema: { docs, docAccess, connectedOrgs } });
}

export type Db = ReturnType<typeof getDb>;

export async function upsertDoc(
  db: Db,
  doc: {
    spaceUri: string;
    docId: string;
    ownerDid: string;
    title: string;
    isOrg?: boolean;
  },
): Promise<void> {
  const now = Date.now();
  await db
    .insert(docs)
    .values({ ...doc, isOrg: doc.isOrg ?? false, updatedAt: now })
    .onConflictDoUpdate({
      target: docs.spaceUri,
      // isOrg is deliberately omitted unless the caller passes it: a
      // conflict only updates the columns listed here, so a caller that
      // doesn't have (or care about) an opinion on isOrg — docRoom.ts's
      // content-flush upsert, notably — leaves the existing row's value
      // alone instead of silently resetting it to false.
      set: {
        docId: doc.docId,
        ownerDid: doc.ownerDid,
        title: doc.title,
        updatedAt: now,
        ...(doc.isOrg !== undefined ? { isOrg: doc.isOrg } : {}),
      },
    });
}

function toSummary(r: typeof docs.$inferSelect): DocSummary {
  return {
    docId: r.docId,
    uri: r.spaceUri,
    ownerDid: r.ownerDid,
    title: r.title,
    isOrg: r.isOrg,
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
      isOrg: docs.isOrg,
    })
    .from(docs)
    .innerJoin(docAccess, eq(docs.spaceUri, docAccess.spaceUri))
    .where(eq(docAccess.subjectDid, subjectDid))
    .orderBy(desc(docs.updatedAt));
  return rows.map(toSummary);
}

// docsForOrg returns every doc owned by org, regardless of who created it
// or any doc_access grant — org docs have none (see createDoc's org-mode
// branch), since access is org-wide by construction (the space's own
// community.opensocial.access record, not a per-user relation).
export async function docsForOrg(db: Db, org: string): Promise<DocSummary[]> {
  const rows = await db
    .select({
      spaceUri: docs.spaceUri,
      docId: docs.docId,
      ownerDid: docs.ownerDid,
      title: docs.title,
      updatedAt: docs.updatedAt,
      isOrg: docs.isOrg,
    })
    .from(docs)
    .where(and(eq(docs.isOrg, true), eq(docs.ownerDid, org)))
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

// upsertConnectedOrg records that memberDid successfully connected orgDid
// (see session.org-callback.tsx).
export async function upsertConnectedOrg(
  db: Db,
  connection: { memberDid: string; orgDid: string; orgName: string },
): Promise<void> {
  const row = { ...connection, connectedAt: Date.now() };
  await db
    .insert(connectedOrgs)
    .values(row)
    .onConflictDoUpdate({
      target: [connectedOrgs.memberDid, connectedOrgs.orgDid],
      set: { orgName: row.orgName, connectedAt: row.connectedAt },
    });
}

// connectedOrgNames maps each already-connected orgDid (among orgIds) to
// the name recorded for it — an org only needs the OAuth admin-approval
// round-trip once (any admin's connection covers the whole org, since it's
// org-wide sap state, not per-member), so the /orgs picker shows an org as
// already added for every member once one of them has done it, and reuses
// the name recorded then instead of re-fetching it from the org's PDS on
// every listMyOrgs call. Callers pass the current member's own org
// memberships (orgIds) so this only checks orgs relevant to them, rather
// than scanning every org anyone has ever connected. An org connected by
// more than one member takes the most recently recorded name.
export async function connectedOrgNames(
  db: Db,
  orgIds: string[],
): Promise<Map<string, string>> {
  if (orgIds.length === 0) return new Map();
  const rows = await db
    .select({ orgDid: connectedOrgs.orgDid, orgName: connectedOrgs.orgName })
    .from(connectedOrgs)
    .where(inArray(connectedOrgs.orgDid, orgIds))
    .orderBy(desc(connectedOrgs.connectedAt));
  const names = new Map<string, string>();
  for (const row of rows) {
    if (!names.has(row.orgDid)) names.set(row.orgDid, row.orgName);
  }
  return names;
}
