import {
  applyResolution,
  deleteComment,
  deleteCommentReply,
  deleteDocAccess,
  docByUri,
  getDb,
  upsertComment,
  upsertCommentReply,
  upsertDocAccess,
  type Db,
} from "../db";
import {
  COMMENT_COLLECTION,
  COMMENT_REPLY_COLLECTION,
  COMMENT_RESOLUTION_COLLECTION,
  decodeAnchorBytes,
  docSpaceUriForComments,
} from "./comments.server";
import { SapClient } from "./sapClient";
import { AtUri } from "@atproto/syntax";

// OutboxMessage is sap's wire format for a single outbox event, delivered as
// a webhook POST body (see cmd/sap/webhook.go webhookPayload). A JSON-null
// value is a delete tombstone: the record at uri was removed.
export interface OutboxMessage {
  id: number;
  uri: string;
  value: unknown;
}

const CRDT_COLLECTION = "network.habitat.docs.crdt";
const USER_RELATION_COLLECTION = "network.habitat.relationship.userRelation";

// processOutboxMessage routes one outbox event delivered by sap's webhook
// (cmd/sap/webhook.go). Messages this deliberately ignores (wrong
// collection, unknown doc, missing blob ref) return normally, which the
// caller (webhook.ts's handleSapWebhook) turns into a 200 so sap acks them
// immediately — one uninteresting message must not be able to wedge the
// outbox. A malformed uri (not a space-record uri at all) throws instead,
// which handleSapWebhook turns into a 500 so sap retries it.
export async function processOutboxMessage(
  env: Env,
  msg: OutboxMessage,
): Promise<void> {
  const atUri = new AtUri(msg.uri);
  const spaceRef = atUri.spaceRef();
  const { authorDid, collection, rkey } = atUri;
  if (!spaceRef || !authorDid || !collection || !rkey) return;
  const spaceUri = spaceRef.toString();

  if (collection === USER_RELATION_COLLECTION) {
    await handleUserRelation(env, msg.uri, spaceUri, msg.value);
    return;
  }
  if (collection === COMMENT_COLLECTION) {
    await handleComment(env, msg.uri, spaceUri, authorDid, rkey, msg.value);
    return;
  }
  if (collection === COMMENT_REPLY_COLLECTION) {
    await handleCommentReply(env, msg.uri, spaceUri, authorDid, msg.value);
    return;
  }
  if (collection === COMMENT_RESOLUTION_COLLECTION) {
    await handleCommentResolution(env, msg.uri, spaceUri, authorDid, msg.value);
    return;
  }
  if (collection !== CRDT_COLLECTION) return;
  const value = (msg.value ?? {}) as { blob?: { ref?: { $link?: string } } };
  const cid = value.blob?.ref?.$link;
  if (!cid) return;
  const doc = await docByUri(getDb(env), spaceUri);
  if (!doc) return; // this deployment does not know this doc
  const stub = env.DOC.get(env.DOC.idFromName(spaceUri));
  await stub.applyRemote({ spaceUri, ownerDid: doc.ownerDid }, cid);
}

// handleUserRelation mirrors a network.habitat.relationship.userRelation
// record into doc_access: a live record means a doc's access changed, a
// JSON-null value means the record was deleted (sap's delete tombstone —
// see pkg/sap/syncer/sync.go). The tombstone carries no subject, only the
// record's own URI, which is why doc_access keeps that URI as a lookup
// column even though it isn't the primary key.
async function handleUserRelation(
  env: Env,
  uri: string,
  spaceUri: string,
  value: unknown,
): Promise<void> {
  const db = getDb(env);
  if (value === null) {
    await deleteDocAccess(db, uri);
    return;
  }
  const record = value as { subject?: string; relation?: string };
  if (!record.subject || !record.relation) return;
  await upsertDocAccess(db, {
    uri,
    spaceUri,
    subjectDid: record.subject,
    relation: record.relation,
  });
}

// docForComments resolves the doc a comments-space record belongs to,
// shared by all three comment-record handlers below.
async function docForComments(db: Db, spaceUri: string) {
  const docSpaceUri = docSpaceUriForComments(spaceUri);
  if (!docSpaceUri) return undefined;
  const doc = await docByUri(db, docSpaceUri);
  if (!doc) return undefined; // this deployment does not know this doc
  return { docSpaceUri, doc };
}

// handleComment mirrors a network.habitat.docs.comment record — a
// thread's root — into the comments table, the same way handleUserRelation
// does for access grants: a live record is an upsert, a JSON-null value is
// the delete tombstone.
//
// The outbox message itself carries no CID (see cmd/sap/webhook.go's
// webhookPayload), but the comments table's cid column is what lets a
// reply or resolution action build the com.atproto.repo.strongRef it
// needs to reference this comment — so this fetches the record directly
// (network.habitat.space.getRecord, which does return a cid) rather than
// trusting the outbox's own value for this one field. Authenticated as the
// doc's owner, the same way DocRoom.applyRemote reads content for records
// it didn't author itself — the owner always holds at least reader on the
// comments space (it inherits from the doc space, which the owner owns).
//
// The author is the repo the record lives in, not a field on the record:
// a comment record can only be written into its own author's repo, so the
// URI is the authoritative claim about who wrote it. A `author` field in
// the record body would be a self-asserted one.
async function handleComment(
  env: Env,
  uri: string,
  spaceUri: string,
  repo: string,
  rkey: string,
  value: unknown,
): Promise<void> {
  const db = getDb(env);
  if (value === null) {
    await deleteComment(db, uri);
    return;
  }
  const resolved = await docForComments(db, spaceUri);
  if (!resolved) return;
  const { docSpaceUri, doc } = resolved;

  const record = value as {
    body?: string;
    anchorStart?: unknown;
    anchorEnd?: unknown;
    quotedText?: string;
    createdAt?: string;
  };
  // anchorStart/anchorEnd are the lexicon "bytes" type, which marshals
  // over JSON as {"$bytes": "<base64>"} — see comments.server.ts's
  // decodeAnchorBytes — not a plain string.
  const anchorStart = decodeAnchorBytes(record.anchorStart);
  const anchorEnd = decodeAnchorBytes(record.anchorEnd);
  if (typeof record.body !== "string" || !anchorStart || !anchorEnd) {
    return;
  }

  const client = new SapClient(env, doc.ownerDid);
  let cid: string;
  try {
    const got = await client.call<{ cid: string }>(
      "network.habitat.space.getRecord",
      "GET",
      { space: spaceUri, repo, collection: COMMENT_COLLECTION, rkey },
    );
    cid = got.cid;
  } catch {
    return; // can't mirror without a cid to hand out for strongRefs
  }

  // createdAt is the record's own claim about when it was written; it
  // orders a thread, so an unparseable one falls back to now rather than
  // NaN (which would sort unpredictably).
  const createdAt = Date.parse(record.createdAt ?? "");
  await upsertComment(db, {
    uri,
    cid,
    docSpaceUri,
    authorDid: repo,
    body: record.body,
    anchorStart,
    anchorEnd,
    quotedText: record.quotedText ?? null,
    createdAt: Number.isNaN(createdAt) ? Date.now() : createdAt,
  });
}

// handleCommentReply mirrors a network.habitat.docs.commentReply record
// into comment_replies. Unlike a root comment, a reply is never itself the
// target of a strongRef, so this doesn't need its cid — no extra fetch.
async function handleCommentReply(
  env: Env,
  uri: string,
  spaceUri: string,
  repo: string,
  value: unknown,
): Promise<void> {
  const db = getDb(env);
  if (value === null) {
    await deleteCommentReply(db, uri);
    return;
  }
  const resolved = await docForComments(db, spaceUri);
  if (!resolved) return;
  const { docSpaceUri } = resolved;

  const record = value as {
    comment?: { uri?: string };
    body?: string;
    createdAt?: string;
  };
  if (!record.comment?.uri || typeof record.body !== "string") return;

  const createdAt = Date.parse(record.createdAt ?? "");
  await upsertCommentReply(db, {
    uri,
    docSpaceUri,
    commentUri: record.comment.uri,
    authorDid: repo,
    body: record.body,
    createdAt: Number.isNaN(createdAt) ? Date.now() : createdAt,
  });
}

// handleCommentResolution mirrors a network.habitat.docs.commentResolution
// record — a resolve/reopen action, not a comment — into the
// comment_resolutions table via applyResolution's last-write-wins merge.
// Unlike handleComment/handleUserRelation, a delete tombstone here is
// simply ignored: retracting a past resolve/reopen action isn't a
// supported operation (there's no "undo", only taking a new action), and
// the mirror holds only the current status, not a history of actions, so
// there'd be nothing meaningful to fall back to.
async function handleCommentResolution(
  env: Env,
  uri: string,
  spaceUri: string,
  repo: string,
  value: unknown,
): Promise<void> {
  if (value === null) return;
  const db = getDb(env);
  const resolved = await docForComments(db, spaceUri);
  if (!resolved) return;
  const { docSpaceUri } = resolved;

  const record = value as {
    comment?: { uri?: string };
    resolved?: boolean;
    createdAt?: string;
  };
  if (!record.comment?.uri || typeof record.resolved !== "boolean") return;

  const createdAt = Date.parse(record.createdAt ?? "");
  await applyResolution(db, {
    docSpaceUri,
    commentUri: record.comment.uri,
    uri,
    resolverDid: repo,
    resolved: record.resolved,
    createdAt: Number.isNaN(createdAt) ? Date.now() : createdAt,
  });
}
