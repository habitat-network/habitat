import { docByUri, getDb } from "../db";
import { parseSpaceRecordUri, type OutboxMessage } from "./spaceUri";

const CRDT_COLLECTION = "network.habitat.docs.crdt";

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
  if (!parsed || parsed.collection !== CRDT_COLLECTION) return;
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
