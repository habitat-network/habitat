import { drizzle } from "drizzle-orm/d1";
import { and, asc, desc, eq, inArray } from "drizzle-orm";
import {
  docs,
  docAccess,
  connectedOrgs,
  comments,
  commentReplies,
  commentResolutions,
} from "./schema";

export interface DocSummary {
  docId: string;
  uri: string;
  ownerDid: string;
  title: string;
  isOrg: boolean;
}

export function getDb(env: { DB: D1Database }) {
  return drizzle(env.DB, {
    schema: {
      docs,
      docAccess,
      connectedOrgs,
      comments,
      commentReplies,
      commentResolutions,
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
// the comment. cid is stored so a reply or resolution action can build the
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

export interface CommentWithResolution extends CommentRow {
  resolved: boolean;
}

// commentsForDocWithResolution is commentsForDoc plus each thread's
// current resolve state, left-joined in from comment_resolutions in one
// query rather than fetched separately and matched up in application code.
// A comment with no resolution action at all (the common case) comes back
// with resolved: false, its default unactioned state.
export async function commentsForDocWithResolution(
  db: Db,
  docSpaceUri: string,
): Promise<CommentWithResolution[]> {
  const rows = await db
    .select({
      uri: comments.uri,
      cid: comments.cid,
      docSpaceUri: comments.docSpaceUri,
      authorDid: comments.authorDid,
      body: comments.body,
      anchorStart: comments.anchorStart,
      anchorEnd: comments.anchorEnd,
      quotedText: comments.quotedText,
      createdAt: comments.createdAt,
      resolved: commentResolutions.resolved,
    })
    .from(comments)
    .leftJoin(
      commentResolutions,
      and(
        eq(comments.uri, commentResolutions.commentUri),
        eq(comments.docSpaceUri, commentResolutions.docSpaceUri),
      ),
    )
    .where(eq(comments.docSpaceUri, docSpaceUri))
    .orderBy(asc(comments.createdAt), asc(comments.uri));
  return rows.map((r) => ({ ...r, resolved: r.resolved ?? false }));
}

// commentByUri looks up a single root comment by its own URI — used to
// resolve the strongRef a reply or resolution action needs to reference it
// (uri + cid) when the caller only has the URI in hand.
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
// deleted record. Its replies and resolution actions are left as-is (they
// reference the URI, which still identifies the thread even once the root
// record is gone) — cleaning those up isn't implemented; deleting a
// thread's root is not itself a supported operation yet.
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

export interface CommentResolutionRow {
  docSpaceUri: string;
  commentUri: string;
  uri: string;
  resolverDid: string;
  resolved: boolean;
  createdAt: number;
}

// applyResolution records a network.habitat.docs.commentResolution action —
// a resolve or reopen, written by whoever performed it (see that lexicon's
// comment on why this isn't a field on the comment record itself). Only
// the most recent action per (docSpaceUri, commentUri) is kept: a stale
// action arriving after a newer one (e.g. the outbox redelivering an old
// message, or two resolves racing) is a no-op rather than clobbering the
// newer state — last-write-wins by createdAt, not by delivery order.
export async function applyResolution(
  db: Db,
  resolution: CommentResolutionRow,
): Promise<void> {
  const [existing] = await db
    .select({ createdAt: commentResolutions.createdAt })
    .from(commentResolutions)
    .where(
      and(
        eq(commentResolutions.docSpaceUri, resolution.docSpaceUri),
        eq(commentResolutions.commentUri, resolution.commentUri),
      ),
    )
    .limit(1);
  if (existing && existing.createdAt > resolution.createdAt) return;

  await db
    .insert(commentResolutions)
    .values(resolution)
    .onConflictDoUpdate({
      target: [commentResolutions.docSpaceUri, commentResolutions.commentUri],
      set: {
        uri: resolution.uri,
        resolverDid: resolution.resolverDid,
        resolved: resolution.resolved,
        createdAt: resolution.createdAt,
      },
    });
}

// resolutionsForDoc returns the current resolve/reopen status of every
// thread on a doc that has ever had a commentResolution action taken on
// it — a thread absent from the result is simply unresolved (its default,
// unactioned state).
export async function resolutionsForDoc(
  db: Db,
  docSpaceUri: string,
): Promise<CommentResolutionRow[]> {
  return db
    .select()
    .from(commentResolutions)
    .where(eq(commentResolutions.docSpaceUri, docSpaceUri));
}
