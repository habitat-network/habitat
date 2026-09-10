import { AtUri, SpaceRef } from "@atproto/syntax";
import {
  deleteDocAccess,
  deleteDocOrgAccess,
  docByUri,
  getDb,
  upsertDoc,
  upsertDocAccess,
  upsertDocOrgAccess,
  type DocSummary,
} from "../db";
import { querySpace } from "./sapClient";

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
  if (!spaceRef || !atUri.authorDid || !atUri.collection || !atUri.rkey) return;
  const spaceUri = spaceRef.toString();

  if (atUri.collection === USER_RELATION_COLLECTION) {
    await handleUserRelation(env, msg.uri, spaceUri, msg.value);
    return;
  }
  if (atUri.collection === SPACE_RELATION_COLLECTION) {
    await handleSpaceRelation(env, msg.uri, spaceUri, msg.value);
    return;
  }
  if (atUri.collection === MARKDOWN_COLLECTION) {
    await handleMarkdown(env, spaceRef, msg.value);
    return;
  }
  if (atUri.collection !== CRDT_COLLECTION) return;
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
