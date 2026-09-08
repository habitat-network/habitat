import {
  deleteComment,
  deleteDocAccess,
  docByUri,
  getDb,
  upsertComment,
  upsertDocAccess,
} from "../db";
import {
  COMMENT_COLLECTION,
  docSpaceUriForComments,
} from "./comments.server";
import { parseSpaceRecordUri, type OutboxMessage } from "./spaceUri";

const CRDT_COLLECTION = "network.habitat.docs.crdt";
const USER_RELATION_COLLECTION = "network.habitat.relationship.userRelation";

// processOutboxMessage routes one outbox event delivered by sap's webhook
// (cmd/sap/webhook.go). Messages this deliberately ignores (wrong
// collection, unknown doc, missing blob ref) return normally, which the
// caller (webhook.ts's handleSapWebhook) turns into a 200 so sap acks them
// immediately — one uninteresting message must not be able to wedge the
// outbox.
export async function processOutboxMessage(
  env: Env,
  msg: OutboxMessage,
): Promise<void> {
  const parsed = parseSpaceRecordUri(msg.uri);
  if (!parsed) return;
  if (parsed.collection === USER_RELATION_COLLECTION) {
    await handleUserRelation(env, msg.uri, parsed.spaceUri, msg.value);
    return;
  }
  if (parsed.collection === COMMENT_COLLECTION) {
    await handleComment(env, msg.uri, parsed.spaceUri, parsed.repo, msg.value);
    return;
  }
  if (parsed.collection !== CRDT_COLLECTION) return;
  const value = (msg.value ?? {}) as { blob?: { ref?: { $link?: string } } };
  const cid = value.blob?.ref?.$link;
  if (!cid) return;
  const doc = await docByUri(getDb(env), parsed.spaceUri);
  if (!doc) return; // this deployment does not know this doc
  const stub = env.DOC.get(env.DOC.idFromName(parsed.spaceUri));
  await stub.applyRemote(
    { spaceUri: parsed.spaceUri, ownerDid: doc.ownerDid },
    cid,
  );
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

// handleComment mirrors a network.habitat.docs.comment record into the
// comments table, the same way handleUserRelation does for access grants:
// a live record is an upsert, a JSON-null value is the delete tombstone.
// Comments live in the doc's companion comments space, so the doc they
// belong to is derived from that space's URI rather than read off the
// record — the record itself doesn't name its doc, and doesn't need to.
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
  value: unknown,
): Promise<void> {
  const db = getDb(env);
  if (value === null) {
    await deleteComment(db, uri);
    return;
  }
  const docSpaceUri = docSpaceUriForComments(spaceUri);
  if (!docSpaceUri) return;
  const doc = await docByUri(db, docSpaceUri);
  if (!doc) return; // this deployment does not know this doc

  const record = value as {
    threadId?: string;
    body?: string;
    quotedText?: string;
    resolved?: boolean;
    createdAt?: string;
  };
  if (!record.threadId || typeof record.body !== "string") return;

  // createdAt is the record's own claim about when it was written; it
  // orders a thread, so an unparseable one falls back to now rather than
  // NaN (which would sort unpredictably).
  const createdAt = Date.parse(record.createdAt ?? "");
  await upsertComment(db, {
    uri,
    docSpaceUri,
    threadId: record.threadId,
    authorDid: repo,
    body: record.body,
    quotedText: record.quotedText ?? null,
    resolved: record.resolved ?? false,
    createdAt: Number.isNaN(createdAt) ? Date.now() : createdAt,
  });
}
