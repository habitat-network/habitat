import { drizzle } from "drizzle-orm/d1";
import {
  and,
  asc,
  desc,
  eq,
  inArray,
  isNotNull,
  notInArray,
  or,
} from "drizzle-orm";
import {
  docs,
  docAccess,
  docOrgAccess,
  connectedOrgs,
  comments,
  commentReplies,
} from "./schema";

export interface DocSummary {
  docId: string;
  uri: string;
  ownerDid: string;
  title: string;
}

export function getDb(env: { DB: D1Database }) {
  return drizzle(env.DB, {
    schema: {
      docs,
      docAccess,
      docOrgAccess,
      connectedOrgs,
      comments,
      commentReplies,
    },
  });
}

export type Db = ReturnType<typeof getDb>;

export async function upsertDoc(
  db: Db,
  doc: {
    spaceUri: string;
    docId: string;
    ownerDid: string;
    title: string;
  },
): Promise<void> {
  const now = Date.now();
  await db
    .insert(docs)
    .values({ ...doc, updatedAt: now })
    .onConflictDoUpdate({
      target: docs.spaceUri,
      set: {
        docId: doc.docId,
        ownerDid: doc.ownerDid,
        title: doc.title,
        updatedAt: now,
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

// docsFor returns the docs a subject can open, per the
// doc_access rows synced from network.habitat.relationship.userRelation
// (see outbox.ts), and — in org mode — the doc_org_access rows that share a
// doc with a whole org.
//
// Both modes are the same query, differing only in which docs are in scope.
// A doc's ownerDid is its space authority (the <did> in
// at://<did>/space/<type>/<skey>), so it says which identity the doc
// belongs to: in org mode that's the org itself, and org mode lists the
// org's docs the member can reach — shared with the whole org, or granted
// to them personally, which covers a doc they created but haven't shared.
//
// Personal mode is the complement: docs belonging to a person rather than
// an org, which is why it excludes any doc whose authority is an org this
// deployment knows (connected_orgs — an org's docs only reach this DB once
// somebody connects it). Without that an org doc would show up in its
// creator's personal list, since they hold a personal grant on it. There's
// no org to match either, so the org join finds nothing (no doc_org_access
// row has an empty org DID) and only personal grants remain.
//
// Each join matches at most one row (both are keyed by their table's
// primary key), so a doc reachable both ways still appears once.
export async function docsFor(
  db: Db,
  subjectDid: string,
  orgDid?: string,
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
    .leftJoin(
      docAccess,
      and(
        eq(docs.spaceUri, docAccess.spaceUri),
        eq(docAccess.subjectDid, subjectDid),
      ),
    )
    .leftJoin(
      docOrgAccess,
      and(
        eq(docs.spaceUri, docOrgAccess.spaceUri),
        eq(docOrgAccess.orgDid, orgDid ?? ""),
      ),
    )
    .where(
      and(
        orgDid
          ? eq(docs.ownerDid, orgDid)
          : notInArray(
              docs.ownerDid,
              db.select({ orgDid: connectedOrgs.orgDid }).from(connectedOrgs),
            ),
        or(isNotNull(docAccess.spaceUri), isNotNull(docOrgAccess.spaceUri)),
      ),
    )
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

// upsertDocOrgAccess records or updates a doc's org-wide grant, keyed by
// (orgDid, spaceUri) — an org holds at most one relation on a given doc.
export async function upsertDocOrgAccess(
  db: Db,
  access: {
    uri: string;
    spaceUri: string;
    orgDid: string;
    relation: string;
  },
): Promise<void> {
  const row = { ...access, updatedAt: Date.now() };
  await db
    .insert(docOrgAccess)
    .values(row)
    .onConflictDoUpdate({
      target: [docOrgAccess.orgDid, docOrgAccess.spaceUri],
      set: {
        uri: row.uri,
        relation: row.relation,
        updatedAt: row.updatedAt,
      },
    });
}

// deleteDocOrgAccess removes a doc's org-wide grant by the spaceRelation
// record's own URI, mirroring deleteDocAccess.
export async function deleteDocOrgAccess(db: Db, uri: string): Promise<void> {
  await db.delete(docOrgAccess).where(eq(docOrgAccess.uri, uri));
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

export interface CommentRow {
  uri: string;
  cid: string;
  docSpaceUri: string;
  authorDid: string;
  body: string;
  anchorStart: string;
  anchorEnd: string;
  quotedText: string | null;
  createdAt: number;
}

// upsertComment mirrors one network.habitat.docs.comment record — the root
// of a thread — into the comments table. Keyed by the record's own URI, so
// re-delivery of the same record from the outbox (which sap retries until
// chalk 200s — see webhook.ts) updates in place rather than duplicating
// the comment. cid is stored so a reply can build the
// com.atproto.repo.strongRef it needs without a separate read.
export async function upsertComment(
  db: Db,
  comment: {
    uri: string;
    cid: string;
    docSpaceUri: string;
    authorDid: string;
    body: string;
    anchorStart: string;
    anchorEnd: string;
    quotedText?: string | null;
    createdAt?: number;
  },
): Promise<void> {
  const row = {
    ...comment,
    quotedText: comment.quotedText ?? null,
    createdAt: comment.createdAt ?? Date.now(),
  };
  await db
    .insert(comments)
    .values(row)
    .onConflictDoUpdate({
      target: comments.uri,
      set: {
        cid: row.cid,
        docSpaceUri: row.docSpaceUri,
        authorDid: row.authorDid,
        body: row.body,
        anchorStart: row.anchorStart,
        anchorEnd: row.anchorEnd,
        quotedText: row.quotedText,
        createdAt: row.createdAt,
      },
    });
}

// commentsForDoc returns every thread-starting comment on a doc, oldest
// first. Ordered by (createdAt, uri) rather than createdAt alone so two
// comments written in the same millisecond still come back in a stable
// order across calls.
export async function commentsForDoc(
  db: Db,
  docSpaceUri: string,
): Promise<CommentRow[]> {
  return db
    .select()
    .from(comments)
    .where(eq(comments.docSpaceUri, docSpaceUri))
    .orderBy(asc(comments.createdAt), asc(comments.uri));
}

// commentByUri looks up a single root comment by its own URI — used to
// resolve the strongRef a reply needs to reference it (uri + cid) when the
// caller only has the URI in hand.
export async function commentByUri(
  db: Db,
  uri: string,
): Promise<CommentRow | undefined> {
  const [row] = await db
    .select()
    .from(comments)
    .where(eq(comments.uri, uri))
    .limit(1);
  return row;
}

// deleteComment removes a comment by its record URI, in response to either
// an explicit delete or the JSON-null tombstone the outbox emits for a
// deleted record. Its replies are left as-is (they reference the URI,
// which still identifies the thread even once the root record is gone) —
// cleaning those up isn't implemented; deleting a thread's root is not
// itself a supported operation yet.
export async function deleteComment(db: Db, uri: string): Promise<void> {
  await db.delete(comments).where(eq(comments.uri, uri));
}

export interface CommentReplyRow {
  uri: string;
  docSpaceUri: string;
  commentUri: string;
  authorDid: string;
  body: string;
  createdAt: number;
}

// upsertCommentReply mirrors one network.habitat.docs.commentReply record.
// Keyed by the reply's own URI, same reasoning as upsertComment.
export async function upsertCommentReply(
  db: Db,
  reply: {
    uri: string;
    docSpaceUri: string;
    commentUri: string;
    authorDid: string;
    body: string;
    createdAt?: number;
  },
): Promise<void> {
  const row = { ...reply, createdAt: reply.createdAt ?? Date.now() };
  await db
    .insert(commentReplies)
    .values(row)
    .onConflictDoUpdate({
      target: commentReplies.uri,
      set: {
        docSpaceUri: row.docSpaceUri,
        commentUri: row.commentUri,
        authorDid: row.authorDid,
        body: row.body,
        createdAt: row.createdAt,
      },
    });
}

// repliesForDoc returns every reply on a doc, oldest first — grouped by
// commentUri client-side the same way commentsForDoc's rows are grouped
// into threads.
export async function repliesForDoc(
  db: Db,
  docSpaceUri: string,
): Promise<CommentReplyRow[]> {
  return db
    .select()
    .from(commentReplies)
    .where(eq(commentReplies.docSpaceUri, docSpaceUri))
    .orderBy(asc(commentReplies.createdAt), asc(commentReplies.uri));
}

// deleteCommentReply removes a reply by its record URI.
export async function deleteCommentReply(db: Db, uri: string): Promise<void> {
  await db.delete(commentReplies).where(eq(commentReplies.uri, uri));
}
