import { constructSpaceURI, parseSpaceURI } from "internal";
import {
  deleteComment,
  setThreadResolved,
  upsertComment,
  type CommentRow,
  type Db,
} from "../db";
import { DOCS_SPACE_TYPE } from "./functions.server";
import { parseSpaceRecordUri } from "./spaceUri";
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

// commentsSpaceUri derives the URI of a doc's comments space from the doc's
// own space URI: same owner, same space key, comments space type. Deriving
// it rather than storing it means any chalk instance holding a docId can
// name the comments space without a lookup — the same reason docId is the
// doc's full space URI in the first place (see createDoc). Returns
// undefined if docId isn't a well-formed space URI.
export function commentsSpaceUri(docId: string): string | undefined {
  const parts = parseSpaceURI(docId);
  if (!parts) return undefined;
  return constructSpaceURI({
    spaceOwner: parts.spaceOwner,
    spaceType: COMMENTS_SPACE_TYPE,
    spaceKey: parts.spaceKey,
  });
}

// SPACE_RELATIONS is the inheritance the comments space is created with:
// the doc space's readers become the comments space's readers, and its
// writers become its writers. So sharing the doc shares its comments, and
// revoking access to the doc revokes access to its comments, with no
// second grant to keep in sync — a spaceRelation is a live userset, not a
// snapshot of who held the role when it was written.
const SPACE_RELATIONS: { subjectRole: "reader" | "writer" }[] = [
  { subjectRole: "reader" },
  { subjectRole: "writer" },
];

// ensureCommentsSpace creates a doc's comments space and its inheritance
// from the doc space, and is safe to call repeatedly: creating a space that
// already exists fails with SpaceAlreadyExists, and re-setting a relation
// that already exists is a no-op write (setSpaceRelation is an upsert on
// the same subject/role pair). Both are swallowed rather than surfaced,
// because every caller's real question is "is the space there now", not
// "did this call create it" — a genuine failure resurfaces on the
// putRecord/listRecords that follows.
//
// Org docs pass the org's DID as owner and reach createSpace through
// Atproto-Proxy, exactly as createDocSpace does for the doc space itself.
export async function ensureCommentsSpace(
  client: SapClient,
  docId: string,
  opts: { ownerDid: string; isOrg: boolean },
): Promise<string | undefined> {
  const parts = parseSpaceURI(docId);
  const spaceUri = commentsSpaceUri(docId);
  if (!parts || !spaceUri) return undefined;

  try {
    if (opts.isOrg) {
      await client.call(
        "community.opensocial.createSpace",
        "POST",
        {
          org: opts.ownerDid,
          type: COMMENTS_SPACE_TYPE,
          skey: parts.spaceKey,
          roles: ["admin", "member"],
        },
        { atprotoProxy: `${opts.ownerDid}#habitat` },
      );
    } else {
      await client.call("network.habitat.simplespace.createSpace", "POST", {
        did: opts.ownerDid,
        type: COMMENTS_SPACE_TYPE,
        skey: parts.spaceKey,
      });
    }
  } catch {
    // Already exists (the common case on every call after the first).
  }

  for (const { subjectRole } of SPACE_RELATIONS) {
    try {
      await client.call(
        "network.habitat.relationship.setSpaceRelation",
        "POST",
        {
          subject: docId,
          subjectRole,
          relation: subjectRole,
          space: spaceUri,
        },
      );
    } catch {
      // Already set, or the caller isn't a manager of the comments space —
      // either way the read/write that follows is the real gate.
    }
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
  const parts = parseSpaceURI(commentsSpace);
  if (!parts || parts.spaceType !== COMMENTS_SPACE_TYPE) return undefined;
  return constructSpaceURI({
    spaceOwner: parts.spaceOwner,
    spaceType: DOCS_SPACE_TYPE,
    spaceKey: parts.spaceKey,
  });
}

// parseCommentRecordUri splits a comment record's URI into the parts a
// putRecord/deleteRecord on it needs (its space, the repo holding it, and
// its rkey), rejecting anything that isn't a comment record in a comments
// space — so a caller can't be tricked into rewriting an unrelated record
// by passing its URI to a comment mutation. The rkey comes back this way
// rather than being chosen by chalk: comment records are keyed "tid" and
// pear mints that itself when putRecord is called without an rkey (see
// internal/spaces/store.go's PutRecord), so the URI it returns is the only
// place the key exists.
export function parseCommentRecordUri(uri: string):
  | {
      spaceUri: string;
      docSpaceUri: string;
      repo: string;
      rkey: string;
    }
  | undefined {
  const parsed = parseSpaceRecordUri(uri);
  if (!parsed) return undefined;
  if (parsed.type !== COMMENTS_SPACE_TYPE) return undefined;
  if (parsed.collection !== COMMENT_COLLECTION) return undefined;
  const docSpaceUri = docSpaceUriForComments(parsed.spaceUri);
  if (!docSpaceUri) return undefined;
  return {
    spaceUri: parsed.spaceUri,
    docSpaceUri,
    repo: parsed.repo,
    rkey: parsed.rkey,
  };
}

// CommentView is the shape createComment/listComments hand back to the
// client — a CommentRow (the D1 mirror's own shape) with authorDid
// promoted from bookkeeping into what the UI actually renders.
export interface CommentView {
  uri: string;
  threadId: string;
  authorDid: string;
  body: string;
  quotedText: string | null;
  resolved: boolean;
  createdAt: number;
}

export function toCommentView(row: CommentRow): CommentView {
  return {
    uri: row.uri,
    threadId: row.threadId,
    authorDid: row.authorDid,
    body: row.body,
    quotedText: row.quotedText,
    resolved: row.resolved,
    createdAt: row.createdAt,
  };
}

// writeComment creates the doc's comments space on first use (see
// ensureCommentsSpace), writes the comment record into it, and mirrors it
// into D1 immediately rather than waiting on the outbox webhook to deliver
// the same record back — without this, a commenter's own listComments
// wouldn't show the comment they just wrote until that async round-trip
// lands. The webhook's own upsertComment is a no-op once this has landed
// (same uri). Caller is responsible for the role check (functions.ts's
// createComment requires editor before calling this) — pear would reject
// the putRecord anyway for a non-writer, but checking first turns that
// into a clear "forbidden" instead of a proxied error.
export async function writeComment(
  client: SapClient,
  db: Db,
  did: string,
  docId: string,
  opts: {
    threadId: string;
    body: string;
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
  const { uri } = await client.call<{ uri: string }>(
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
        threadId: opts.threadId,
        body: opts.body,
        ...(opts.quotedText ? { quotedText: opts.quotedText } : {}),
        createdAt: createdAt.toISOString(),
      },
    },
  );

  const row: CommentRow = {
    uri,
    docSpaceUri: docId,
    threadId: opts.threadId,
    authorDid: did,
    body: opts.body,
    quotedText: opts.quotedText ?? null,
    resolved: false,
    createdAt: createdAt.getTime(),
  };
  await upsertComment(db, row);
  return toCommentView(row);
}

// setResolved marks a whole thread resolved (or reopens it), rewriting each
// of the caller's own comment records upstream — a resolve made on one
// chalk instance has to be visible on every other one, which only the
// records carry, not the D1 mirror alone.
export async function setResolved(
  client: SapClient,
  db: Db,
  did: string,
  docId: string,
  threadId: string,
  resolved: boolean,
  thread: CommentRow[],
): Promise<void> {
  for (const comment of thread) {
    // Only the author's own repo holds their comment records, so a resolve
    // can only rewrite this caller's own — the rest are updated in the
    // local mirror below and converge when their authors' records next
    // sync. Rewriting someone else's record isn't possible here (and
    // shouldn't be).
    if (comment.authorDid !== did) continue;
    const parsed = parseCommentRecordUri(comment.uri);
    if (!parsed) continue;
    await client.call("network.habitat.space.putRecord", "POST", {
      space: parsed.spaceUri,
      repo: did,
      collection: COMMENT_COLLECTION,
      rkey: parsed.rkey,
      record: {
        $type: COMMENT_COLLECTION,
        threadId: comment.threadId,
        body: comment.body,
        ...(comment.quotedText ? { quotedText: comment.quotedText } : {}),
        resolved,
        createdAt: new Date(comment.createdAt).toISOString(),
      },
    });
  }
  await setThreadResolved(db, docId, threadId, resolved);
}

// removeComment deletes one comment record and its D1 mirror row. Only its
// own author can: the record lives in their repo, and pear rejects a write
// to anyone else's — checked here first (via the uri itself, which is
// self-describing: see parseCommentRecordUri) so a mismatched caller gets
// a clear "forbidden" rather than a proxied 403.
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
