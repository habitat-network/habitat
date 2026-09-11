import { AtUri, SpaceRef } from "@atproto/syntax";
import { jsonToLex, type JsonValue } from "@atproto/lex-json";
import { network } from "api";
import {
  deleteComment,
  deleteCommentReply,
  deleteDocAccess,
  deleteDocOrgAccess,
  docByUri,
  getDb,
  upsertComment,
  upsertCommentReply,
  upsertDoc,
  upsertDocAccess,
  upsertDocOrgAccess,
  type Db,
  type DocSummary,
} from "../db";
import {
  COMMENT_COLLECTION,
  COMMENT_REPLY_COLLECTION,
  docSpaceUriForComments,
} from "./comments.server";
import { querySpace, SapClient } from "./sapClient";

// OutboxMessage is sap's wire format for a single outbox event, delivered as
// a webhook POST body (see cmd/sap/webhook.go webhookPayload). A JSON-null
// value is a delete tombstone: the record at uri was removed.
export interface OutboxMessage {
  id: number;
  uri: string;
  value: unknown;
}

const CRDT_COLLECTION = "network.habitat.docs.crdt";
const MARKDOWN_COLLECTION = "network.habitat.docs.markdown";
const USER_RELATION_COLLECTION = "network.habitat.relationship.userRelation";
const SPACE_RELATION_COLLECTION = "network.habitat.relationship.spaceRelation";
// The space type whose role-holders are "everyone in the org" — a
// spaceRelation naming one of these as its subject is an org-wide grant.
const MEMBERS_SPACE_TYPE = "community.opensocial.members";

// processOutboxMessage routes one outbox event delivered by sap's webhook
// (cmd/sap/webhook.go). Messages this deliberately ignores (malformed uri,
// wrong collection, missing blob ref) return normally, which the caller
// (webhook.ts's handleSapWebhook) turns into a 200 so sap acks them
// immediately — one uninteresting message must not be able to wedge the
// outbox. A uri that doesn't parse at all is treated the same way: it can
// never become valid on retry, so retrying it with 500s would wedge the
// outbox forever. A doc this deployment doesn't know yet isn't ignored: it
// is indexed (see indexUnknownDoc).
export async function processOutboxMessage(
  env: Env,
  msg: OutboxMessage,
): Promise<void> {
  let atUri: AtUri;
  try {
    atUri = new AtUri(msg.uri);
  } catch {
    return; // not a uri at all
  }
  const spaceRef = atUri.spaceRef();
  const { authorDid, collection, rkey } = atUri;
  if (!spaceRef || !authorDid || !collection || !rkey) return;
  const spaceUri = spaceRef.toString();

  if (collection === USER_RELATION_COLLECTION) {
    await handleUserRelation(env, msg.uri, spaceUri, msg.value);
    return;
  }
  if (collection === SPACE_RELATION_COLLECTION) {
    await handleSpaceRelation(env, msg.uri, spaceUri, msg.value);
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
  if (collection === MARKDOWN_COLLECTION) {
    await handleMarkdown(env, spaceRef, msg.value);
    return;
  }
  if (collection !== CRDT_COLLECTION) return;
  const value = (msg.value ?? {}) as { blob?: { ref?: { $link?: string } } };
  const cid = value.blob?.ref?.$link;
  if (!cid) return;
  const doc =
    (await docByUri(getDb(env), spaceUri)) ??
    (await indexUnknownDoc(env, spaceRef));
  const stub = env.DOC.get(env.DOC.idFromName(spaceUri));
  await stub.applyRemote({ spaceUri, ownerDid: doc.ownerDid }, cid);
}

// indexUnknownDoc adds a docs row for a doc this deployment has never seen —
// e.g. one sap's backfill crawl found after chalk's DB was reset, since only
// createDoc and DocRoom's republish otherwise write that table. The owner is
// the subject of the space's "owner" userRelation, looked up with a space
// credential (see querySpace) since no member is signed in here. When that
// lookup can't answer (sap can't mint a credential, no owner relation) the
// owner falls back to the space's authority DID — what createDoc records for
// an org doc — rather than throwing, since a throw would 500 the webhook and
// wedge sap's outbox on a message that may never become deliverable. The
// title is the caller's when it has one (a markdown record), else "Untitled"
// until the doc's markdown record arrives (see handleMarkdown).
async function indexUnknownDoc(
  env: Env,
  spaceRef: SpaceRef,
  title = "Untitled",
): Promise<DocSummary> {
  const spaceUri = spaceRef.toString();
  let ownerDid: string = spaceRef.spaceDid;
  try {
    const res = await querySpace(
      env,
      spaceUri,
      "network.habitat.relationship.listRelations",
      { space: spaceUri, subjectType: "user", relation: "owner" },
    );
    const { relations } = (await res.json()) as {
      relations: { subject: string }[];
    };
    ownerDid = relations[0]?.subject ?? ownerDid;
  } catch (err) {
    console.warn("[outbox] look up owner of unknown doc", spaceUri, err);
  }
  const doc = { spaceUri, docId: spaceUri, ownerDid, title };
  await upsertDoc(getDb(env), doc);
  return { docId: doc.docId, uri: spaceUri, ownerDid, title: doc.title };
}

// handleMarkdown takes a doc's title from its network.habitat.docs.markdown
// record — the rendered title/content the owner republish writes (see
// DocRoom.republishCanonical) — so a doc synced in from sap is listed under
// its real title instead of "Untitled", without waiting on (or being able to
// perform) a republish of its own. A doc this deployment hasn't seen yet is
// indexed here, since its markdown record can arrive before its crdt record.
// A delete tombstone (JSON null) or a record without a title changes nothing.
async function handleMarkdown(
  env: Env,
  spaceRef: SpaceRef,
  value: unknown,
): Promise<void> {
  const title = (value as { title?: unknown } | null)?.title;
  if (typeof title !== "string") return;
  const db = getDb(env);
  const doc = await docByUri(db, spaceRef.toString());
  if (!doc) {
    await indexUnknownDoc(env, spaceRef, title);
    return;
  }
  await upsertDoc(db, {
    spaceUri: doc.uri,
    docId: doc.docId,
    ownerDid: doc.ownerDid,
    title,
  });
}

// handleUserRelation mirrors a network.habitat.relationship.userRelation
// record into doc_access: a live record means a doc's access changed, a
// JSON-null value means the record was deleted (sap's delete tombstone —
// see pkg/sap/syncer/sync.go). The tombstone carries no subject, only the
// record's own URI, which is why doc_access keeps that URI as a lookup
// column even though it isn't the primary key.
//
// A *writer* grant on a doc's comments space is what a commenter is (see
// functions.ts's ROLE_TO_GRANT), so it's filed under the doc space rather
// than the space the record names: doc_access exists to answer "which docs
// can this subject see", and docsFor joins it against the doc — a
// row keyed by the comments space would match no doc at all, and a
// commenter would never see the doc in their own list.
//
// Only writer, though. Creating the comments space also writes its creator
// an owner relation on it, and that subject already holds one on the doc
// space; mapping both onto the same (subject, doc space) row would have
// the second to arrive overwrite the first's record URI, after which the
// wrong tombstone deletes the row and the right one matches nothing. No
// other relation on the comments space adds access the doc space's own
// grants don't already carry, so ignoring them loses nothing.
function docAccessSpace(
  spaceUri: string,
  relation: string,
): string | undefined {
  const docSpaceUri = docSpaceUriForComments(spaceUri);
  if (!docSpaceUri) return spaceUri; // an ordinary doc-space grant
  return relation === "writer" ? docSpaceUri : undefined;
}

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
  const space = docAccessSpace(spaceUri, record.relation);
  if (!space) return;
  await upsertDocAccess(db, {
    uri,
    spaceUri: space,
    subjectDid: record.subject,
    relation: record.relation,
  });
}

// docForComments resolves the doc a comments-space record belongs to,
// shared by both comment-record handlers below.
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
// reply build the com.atproto.repo.strongRef it needs to reference this
// comment — so this fetches the record directly
// (network.habitat.space.getRecord, which does return a cid) rather than
// trusting the outbox's own value for this one field. Authenticated as the
// doc's owner, the same way DocRoom.applyRemote reads content for records
// it didn't author itself — the owner always holds at least reader on the
// comments space (it inherits from the doc space, which the owner owns).
//
// The record is checked against its lexicon (the generated validateMain)
// after jsonToLex has turned its {"$bytes"} anchors back into Uint8Arrays,
// which is the form the bytes validator — and the comments table — expects.
// Anything that fails validation isn't a comment chalk can render, and is
// ignored like any other uninteresting outbox message.
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

  const validated = network.habitat.docs.comment.$safeValidate(
    jsonToLex(value as JsonValue),
  );
  if (!validated.success) return;
  const record = validated.value;

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

  // createdAt is the record's own claim about when it was written; the
  // lexicon's datetime format guarantees it parses.
  await upsertComment(db, {
    uri,
    cid,
    docSpaceUri,
    authorDid: repo,
    body: record.body,
    anchorStart: record.anchorStart,
    anchorEnd: record.anchorEnd,
    quotedText: record.quotedText ?? null,
    createdAt: Date.parse(record.createdAt),
  });
}

// handleCommentReply mirrors a network.habitat.docs.commentReply record
// into comment_replies. Unlike a root comment, a reply is never itself the
// target of a strongRef, so this doesn't need its cid — no extra fetch.
// Validated against its lexicon the same way handleComment is; the
// strongRef's uri is a space record URI
// (at://<did>/space/<type>/<skey>/<repo>/<collection>/<rkey>), which is why
// @atproto/lexicon is pinned to the spaces alpha alongside @atproto/syntax.
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

  const validated = network.habitat.docs.commentReply.$safeValidate(
    jsonToLex(value as JsonValue),
  );
  if (!validated.success) return;
  const record = validated.value;

  await upsertCommentReply(db, {
    uri,
    docSpaceUri,
    commentUri: record.comment.uri,
    authorDid: repo,
    body: record.body,
    createdAt: Date.parse(record.createdAt),
  });
}

// handleSpaceRelation mirrors the org-wide half of the same picture: a
// network.habitat.relationship.spaceRelation whose subject is an org's
// members space grants the doc to that whole org, so it becomes a
// doc_org_access row (what docsFor lists from in org mode). A spaceRelation
// naming any other space — a group, say — is somebody else's userset and is
// ignored here. As with userRelation, a JSON-null value is sap's delete
// tombstone and carries only the record's own URI.
async function handleSpaceRelation(
  env: Env,
  uri: string,
  spaceUri: string,
  value: unknown,
): Promise<void> {
  const db = getDb(env);
  if (value === null) {
    await deleteDocOrgAccess(db, uri);
    return;
  }
  const record = value as { subject?: string; relation?: string };
  if (!record.subject || !record.relation) return;
  let subject: SpaceRef;
  try {
    subject = SpaceRef.parse(record.subject);
  } catch {
    return; // subject isn't a space ref at all
  }
  if (subject.spaceType !== MEMBERS_SPACE_TYPE) return;
  await upsertDocOrgAccess(db, {
    uri,
    spaceUri,
    orgDid: subject.spaceDid,
    relation: record.relation,
  });
}
