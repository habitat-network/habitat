import { AtUri, SpaceRef } from "@atproto/syntax";
import { fromBase64, toBase64 } from "@atproto/lex-data";
import { encodeLexBytes, parseLexBytes } from "@atproto/lex-json";
import {
  deleteComment,
  deleteCommentReply,
  upsertComment,
  upsertCommentReply,
  type CommentReplyRow,
  type CommentRow,
  type Db,
} from "../db";
import { DOCS_SPACE_TYPE } from "./functions.server";
import type { SapClient } from "./sapClient";

// Server-only comment helpers, kept out of functions.ts for the same reason
// functions.server.ts is (see that file's header comment): only
// createServerFn-wrapped exports may be statically imported from
// client-reachable code.

// COMMENTS_SPACE_TYPE is the space type of a doc's companion comments
// space. Comments deliberately don't live in the doc space itself: a doc
// space's writers are the people who may change the document's own text,
// and we want the two governed as one permission structure without
// conflating them — a comment record landing in the doc space would be
// indistinguishable, to anything reading the space, from document state.
export const COMMENTS_SPACE_TYPE = "network.habitat.docs.comments";
export const COMMENT_COLLECTION = "network.habitat.docs.comment";
export const COMMENT_REPLY_COLLECTION = "network.habitat.docs.commentReply";

// A StrongRef is com.atproto.repo.strongRef's shape: a URI pinned to the
// exact record version (by CID) it refers to. Replies reference a thread's
// root comment this way rather than by a client-chosen thread id, per the
// AT Protocol style guide's guidance on referencing another record.
export interface StrongRef {
  uri: string;
  cid: string;
}

// encodeAnchorBytes wraps a base64 anchor string (see commentAnchor.ts's
// encodeAnchor) into the lexicon "bytes" type's wire shape
// (https://atproto.com/specs/lexicon#bytes) — a {"$bytes": "<base64>"}
// wrapper, not a plain string (indigo's atdata.Bytes on the Go side, which
// this record type is validated against) — using @atproto/lex-json's own
// encoder rather than hand-rolling the wrapper. The comment record's
// anchorStart/anchorEnd fields are declared as bytes rather than string
// precisely so a client can't sneak arbitrary non-anchor data into them
// under the guise of "just text".
export function encodeAnchorBytes(base64: string) {
  return encodeLexBytes(fromBase64(base64));
}

// decodeAnchorBytes unwraps a bytes-typed field back to its base64 anchor
// string, or undefined if the value isn't actually in that shape (a
// malformed/absent field — treated as "no anchor" the same way a missing
// string field would be).
export function decodeAnchorBytes(value: unknown): string | undefined {
  if (typeof value !== "object" || value === null) return undefined;
  const bytes = parseLexBytes(value as Record<string, unknown>);
  return bytes ? toBase64(bytes) : undefined;
}

// commentsSpaceUri derives the URI of a doc's comments space from the doc's
// own space URI: same owner, same space key, comments space type. Deriving
// it rather than storing it means any chalk instance holding a docId can
// name the comments space without a lookup — the same reason docId is the
// doc's full space URI in the first place (see createDoc). Returns
// undefined if docId isn't a well-formed space URI.
export function commentsSpaceUri(docId: string): string | undefined {
  const parts = parseSpaceRef(docId);
  if (!parts) return undefined;
  return new SpaceRef(
    parts.spaceDid,
    COMMENTS_SPACE_TYPE,
    parts.skey,
  ).toString();
}

// parseSpaceRef parses a space URI, returning undefined (rather than
// throwing, like SpaceRef.parse) if it's malformed.
function parseSpaceRef(uri: string): SpaceRef | undefined {
  try {
    return SpaceRef.parse(uri);
  } catch {
    return undefined;
  }
}

// SpaceRole is the vocabulary network.habitat.relationship uses for the
// roles a spaceRelation can grant or draw from.
type SpaceRole = "reader" | "writer" | "manager";

// SPACE_RELATIONS is the inheritance a doc and its comments space are
// wired together with. Each entry reads "everyone holding subjectRole on
// the subject space holds relation on the target space", and `reversed`
// says which of the two spaces is the subject: false is doc → comments,
// true is comments → doc. A spaceRelation is a live userset, not a
// snapshot of who held the role when it was written, so none of this
// needs re-syncing as people are added and removed.
//
// The first two make sharing the doc share its comments: its readers can
// read them, its writers can write them, and revoking doc access revokes
// comment access with it.
//
// The third is what a commenter *is* — someone granted writer on the
// comments space directly, who thereby reads the doc without a second
// grant on the doc space to keep in sync (see shareDoc). It runs the
// other way round, which is why the subject/target can't be implied.
//
// The fourth lets any doc editor add commenters: setUserRelation requires
// manager on the space being granted on, and without this only the
// comments space's creator (the doc owner) would hold that — even though
// the same editor can already add editors and viewers to the doc itself.
const SPACE_RELATIONS: {
  subjectRole: SpaceRole;
  relation: SpaceRole;
  reversed?: boolean;
}[] = [
  { subjectRole: "reader", relation: "reader" },
  { subjectRole: "writer", relation: "writer" },
  { subjectRole: "writer", relation: "reader", reversed: true },
  { subjectRole: "manager", relation: "manager" },
];

// isSpaceAlreadyExists reports whether err is createSpace's expected
// "the space is already there" error rather than a real failure. SapClient
// surfaces a proxied error as an Error carrying the response body, which
// for this case is pear's {"error": "SpaceAlreadyExists"} (see
// internal/pearserver/simplespace_create_space.go).
function isSpaceAlreadyExists(err: unknown): boolean {
  return err instanceof Error && err.message.includes("SpaceAlreadyExists");
}

// ensureCommentsSpace creates a doc's comments space and its inheritance
// from the doc space, and is safe to call repeatedly: creating a space that
// already exists fails with SpaceAlreadyExists, and re-setting a relation
// that already exists is a no-op write (setSpaceRelation is an upsert on
// the same subject/role pair). Those expected outcomes are swallowed,
// because every caller's real question is "is the space there now", not
// "did this call create it". Anything else is logged: it doesn't stop the
// caller (the putRecord/listRecords that follows is the real gate) but it
// is the only place the reason is visible, and a comments space that was
// never created surfaces downstream as an opaque SpaceNotFound.
//
// Both calls are made as the *doc owner*, not as the signed-in member.
// createSpace only accepts the caller's own DID (or their org's), and
// setSpaceRelation requires manager on the comments space, which only its
// owner has — so a non-owner being the first to comment on a doc shared
// with them would otherwise fail both steps and leave them unable to
// comment at all.
//
// Org docs are the exception: the owner is the org DID, which has no sap
// session to authenticate as. They pass the org's DID as owner and reach
// createSpace through Atproto-Proxy as the member, exactly as
// createDocSpace does for the doc space itself.
export async function ensureCommentsSpace(
  client: SapClient,
  docId: string,
  opts: { ownerDid: string; isOrg: boolean },
): Promise<string | undefined> {
  const parts = parseSpaceRef(docId);
  const spaceUri = commentsSpaceUri(docId);
  if (!parts || !spaceUri) return undefined;

  const ownerClient = opts.isOrg ? client : client.asDid(opts.ownerDid);

  try {
    if (opts.isOrg) {
      await ownerClient.call(
        "community.opensocial.createSpace",
        "POST",
        {
          org: opts.ownerDid,
          type: COMMENTS_SPACE_TYPE,
          skey: parts.skey,
          roles: ["admin", "member"],
        },
        { atprotoProxy: `${opts.ownerDid}#habitat` },
      );
    } else {
      await ownerClient.call(
        "network.habitat.simplespace.createSpace",
        "POST",
        {
          did: opts.ownerDid,
          type: COMMENTS_SPACE_TYPE,
          skey: parts.skey,
        },
      );
    }
  } catch (err) {
    // Already exists is the common case on every call after the first.
    if (!isSpaceAlreadyExists(err)) {
      console.error("[comments] create comments space", spaceUri, err);
    }
  }

  for (const { subjectRole, relation, reversed } of SPACE_RELATIONS) {
    const subject = reversed ? spaceUri : docId;
    const space = reversed ? docId : spaceUri;
    try {
      await ownerClient.call(
        "network.habitat.relationship.setSpaceRelation",
        "POST",
        { subject, subjectRole, relation, space },
      );
    } catch (err) {
      // A no-op re-set doesn't throw, so anything here is a real failure:
      // the space wasn't created above, or (org docs) the member isn't a
      // manager of it. The read/write that follows is still the real gate.
      console.error(
        "[comments] set comments space relation",
        subject,
        subjectRole,
        space,
        relation,
        err,
      );
    }
  }

  // sap has no way to discover this space on its own until some member's
  // next session crawl — same reason createDoc tracks the doc space right
  // after creating it. Without this, a comment/reply written by anyone
  // other than whichever chalk instance happens to eagerly mirror its own
  // writes locally (writeComment/writeReply) stays
  // invisible to every other chalk instance's D1 mirror until that crawl
  // catches up.
  try {
    await client.trackSpace(spaceUri);
  } catch (err) {
    // Best-effort, same as the steps above — a transient failure here
    // just means this call falls back to relying on the next crawl.
    console.error("[comments] track comments space", spaceUri, err);
  }

  return spaceUri;
}

// docSpaceUriForComments is commentsSpaceUri's inverse: given a comments
// space's URI, the doc space it belongs to. Used by the outbox consumer,
// which sees comment records addressed by the comments space and has to
// file them under the doc the rest of chalk keys everything by.
export function docSpaceUriForComments(
  commentsSpace: string,
): string | undefined {
  const parts = parseSpaceRef(commentsSpace);
  if (!parts || parts.spaceType !== COMMENTS_SPACE_TYPE) return undefined;
  return new SpaceRef(parts.spaceDid, DOCS_SPACE_TYPE, parts.skey).toString();
}

// parseCommentsRecordUri splits a record URI living in a doc's comments
// space into the parts a putRecord/deleteRecord on it needs (its space,
// the repo holding it, and its rkey), rejecting anything that isn't a
// record of the expected collection in a comments space — so a caller
// can't be tricked into rewriting an unrelated record by passing its URI
// to a comment mutation. The rkey comes back this way rather than being
// chosen by chalk: these records are keyed "tid" and pear mints one itself
// when putRecord is called without an rkey (see internal/spaces/store.go's
// PutRecord), so the URI it returns is the only place the key exists.
export function parseCommentsRecordUri(
  uri: string,
  collection: string,
):
  | {
      spaceUri: string;
      docSpaceUri: string;
      repo: string;
      rkey: string;
    }
  | undefined {
  let atUri: AtUri;
  try {
    atUri = new AtUri(uri);
  } catch {
    return undefined;
  }
  const spaceRef = atUri.spaceRef();
  const { authorDid, rkey } = atUri;
  if (!spaceRef || spaceRef.spaceType !== COMMENTS_SPACE_TYPE) return undefined;
  if (atUri.collection !== collection || !authorDid || !rkey) return undefined;
  const spaceUri = spaceRef.toString();
  const docSpaceUri = docSpaceUriForComments(spaceUri);
  if (!docSpaceUri) return undefined;
  return { spaceUri, docSpaceUri, repo: authorDid, rkey };
}

// parseCommentRecordUri is parseCommentsRecordUri fixed to the root
// comment collection — kept as its own name since it's the common case
// (deleting/referencing a root comment).
export function parseCommentRecordUri(uri: string) {
  return parseCommentsRecordUri(uri, COMMENT_COLLECTION);
}

export interface CommentReplyView {
  uri: string;
  commentUri: string;
  authorDid: string;
  body: string;
  createdAt: number;
}

export function toCommentReplyView(row: CommentReplyRow): CommentReplyView {
  return {
    uri: row.uri,
    commentUri: row.commentUri,
    authorDid: row.authorDid,
    body: row.body,
    createdAt: row.createdAt,
  };
}

// CommentView is the shape createComment/listComments hand back to the
// client — a CommentRow (the D1 mirror's own shape) with authorDid
// promoted from bookkeeping into what the UI actually renders, plus its
// replies nested in rather than two parallel lists the client would
// otherwise have to re-join itself. cid rides along so the client can
// build a StrongRef to this comment (to reply to it) without a second
// round-trip.
export interface CommentView {
  uri: string;
  cid: string;
  authorDid: string;
  body: string;
  anchorStart: string;
  anchorEnd: string;
  quotedText: string | null;
  createdAt: number;
  replies: CommentReplyView[];
}

// toCommentView needs replies passed in rather than deriving them itself:
// a single CommentRow has no way to know them — they come from the
// comment_replies table, joined up in functions.ts's listComments, which
// is the only caller with both in hand. A freshly-written comment
// (writeComment) naturally has none yet.
export function toCommentView(
  row: CommentRow,
  opts: { replies: CommentReplyView[] },
): CommentView {
  return {
    uri: row.uri,
    cid: row.cid,
    authorDid: row.authorDid,
    body: row.body,
    anchorStart: row.anchorStart,
    anchorEnd: row.anchorEnd,
    quotedText: row.quotedText,
    createdAt: row.createdAt,
    replies: opts.replies,
  };
}

// writeComment creates the doc's comments space on first use (see
// ensureCommentsSpace), writes the root comment record — including its
// CRDT anchor, computed client-side from the live Y.Doc before this is
// ever called, since only a client holding that document can derive it —
// and mirrors it into D1 immediately rather than waiting on the outbox
// webhook to deliver the same record back. The webhook's own upsertComment
// is a no-op once this has landed (same uri). Caller is responsible for
// the role check (functions.ts's createComment requires editor before
// calling this) — pear would reject the putRecord anyway for a
// non-writer, but checking first turns that into a clear "forbidden"
// instead of a proxied error.
export async function writeComment(
  client: SapClient,
  db: Db,
  did: string,
  docId: string,
  opts: {
    body: string;
    anchorStart: string;
    anchorEnd: string;
    quotedText?: string;
    ownerDid: string;
    isOrg: boolean;
  },
): Promise<CommentView> {
  const spaceUri = await ensureCommentsSpace(client, docId, {
    ownerDid: opts.ownerDid,
    isOrg: opts.isOrg,
  });
  if (!spaceUri) throw new Error("invalid docId");

  const createdAt = new Date();
  const { uri, cid } = await client.call<{ uri: string; cid: string }>(
    "network.habitat.space.putRecord",
    "POST",
    {
      space: spaceUri,
      repo: did,
      collection: COMMENT_COLLECTION,
      // No rkey: comment records are keyed "tid" and pear mints one itself
      // when putRecord is called without it (internal/spaces/store.go's
      // PutRecord), so the returned URI carries the key.
      record: {
        $type: COMMENT_COLLECTION,
        body: opts.body,
        anchorStart: encodeAnchorBytes(opts.anchorStart),
        anchorEnd: encodeAnchorBytes(opts.anchorEnd),
        ...(opts.quotedText ? { quotedText: opts.quotedText } : {}),
        createdAt: createdAt.toISOString(),
      },
    },
  );

  const row: CommentRow = {
    uri,
    cid,
    docSpaceUri: docId,
    authorDid: did,
    body: opts.body,
    anchorStart: opts.anchorStart,
    anchorEnd: opts.anchorEnd,
    quotedText: opts.quotedText ?? null,
    createdAt: createdAt.getTime(),
  };
  await upsertComment(db, row);
  return toCommentView(row, { replies: [] });
}

// writeReply writes a network.habitat.docs.commentReply record referencing
// the thread's root comment by strongRef (see that lexicon's comment for
// why replies don't carry their own anchor). Mirrors eagerly into D1 for
// the same reason writeComment does.
export async function writeReply(
  client: SapClient,
  db: Db,
  did: string,
  docId: string,
  opts: { comment: StrongRef; body: string },
): Promise<CommentReplyView> {
  const spaceUri = commentsSpaceUri(docId);
  if (!spaceUri) throw new Error("invalid docId");

  const createdAt = new Date();
  const { uri } = await client.call<{ uri: string }>(
    "network.habitat.space.putRecord",
    "POST",
    {
      space: spaceUri,
      repo: did,
      collection: COMMENT_REPLY_COLLECTION,
      record: {
        $type: COMMENT_REPLY_COLLECTION,
        comment: opts.comment,
        body: opts.body,
        createdAt: createdAt.toISOString(),
      },
    },
  );

  const row: CommentReplyRow = {
    uri,
    docSpaceUri: docId,
    commentUri: opts.comment.uri,
    authorDid: did,
    body: opts.body,
    createdAt: createdAt.getTime(),
  };
  await upsertCommentReply(db, row);
  return toCommentReplyView(row);
}

// removeComment deletes one root comment record and its D1 mirror row.
// Only its own author can: the record lives in their repo, and pear
// rejects a write to anyone else's — checked here first (via the uri
// itself, which is self-describing: see parseCommentRecordUri) so a
// mismatched caller gets a clear "forbidden" rather than a proxied 403.
export async function removeComment(
  client: SapClient,
  db: Db,
  did: string,
  uri: string,
): Promise<void> {
  const parsed = parseCommentRecordUri(uri);
  if (!parsed || parsed.repo !== did) throw new Error("forbidden");
  await client.call("network.habitat.space.deleteRecord", "POST", {
    space: parsed.spaceUri,
    repo: did,
    collection: COMMENT_COLLECTION,
    rkey: parsed.rkey,
  });
  // Drop it locally now rather than waiting on the outbox tombstone, for
  // the same reason writeComment mirrors eagerly.
  await deleteComment(db, uri);
}

// removeReply deletes one reply record and its D1 mirror row — same
// author-only rule as removeComment.
export async function removeReply(
  client: SapClient,
  db: Db,
  did: string,
  uri: string,
): Promise<void> {
  const parsed = parseCommentsRecordUri(uri, COMMENT_REPLY_COLLECTION);
  if (!parsed || parsed.repo !== did) throw new Error("forbidden");
  await client.call("network.habitat.space.deleteRecord", "POST", {
    space: parsed.spaceUri,
    repo: did,
    collection: COMMENT_REPLY_COLLECTION,
    rkey: parsed.rkey,
  });
  await deleteCommentReply(db, uri);
}
