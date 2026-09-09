import { AtUri } from "@atproto/syntax";
import { deleteDocAccess, docByUri, getDb, upsertDocAccess } from "../db";

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
  if (!spaceRef || !atUri.authorDid || !atUri.collection || !atUri.rkey) return;
  const spaceUri = spaceRef.toString();

  if (atUri.collection === USER_RELATION_COLLECTION) {
    await handleUserRelation(env, msg.uri, spaceUri, msg.value);
    return;
  }
  if (atUri.collection !== CRDT_COLLECTION) return;
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
