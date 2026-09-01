import { deleteDocAccess, docByUri, getDb, upsertDocAccess } from "../db";
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
