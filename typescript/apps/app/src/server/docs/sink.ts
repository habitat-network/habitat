import { AtUri, SpaceRef, type DidString } from "@atproto/syntax";
import {
  getBlobCidString,
  isBlobRef,
  type LexMap,
  type LexValue,
} from "@atproto/lex";
import { network } from "api";
import type { DelEvent, PutEvent } from "@habitat-network/jetstream";
import { Effect } from "effect";
import type { Domain } from "../stream/domain";
import {
  deleteComment,
  deleteCommentReply,
  deleteDocAccess,
  deleteDocOrgAccess,
  deleteMirroredRecord,
  docByUri,
  getDb,
  mirroredRecordUris,
  upsertComment,
  upsertCommentReply,
  upsertDoc,
  upsertDocAccess,
  upsertDocOrgAccess,
  type Db,
  type DocSummary,
} from "@/db";
import {
  COMMENT_COLLECTION,
  COMMENT_REPLY_COLLECTION,
  docSpaceUriForComments,
} from "./comments.server";
import { createHabitatClient } from "@/server/habitatClient";

// Records written before the spaces alpha moved space URIs to the at:space/
// form still embed the older at://{did}/space/... form, which only lenient
// validation accepts. Goes with the migration to the new form.
const LENIENT = { strict: false };

const CRDT_COLLECTION = network.habitat.docs.crdt.$nsid;
const MARKDOWN_COLLECTION = network.habitat.docs.markdown.$nsid;
const USER_RELATION_COLLECTION =
  network.habitat.relationship.userRelation.$nsid;
const SPACE_RELATION_COLLECTION =
  network.habitat.relationship.spaceRelation.$nsid;

// The collections the doc mirror indexes: crdt state, rendered markdown,
// comments and replies, and the sharing grants.
const DOC_COLLECTIONS = [
  CRDT_COLLECTION,
  MARKDOWN_COLLECTION,
  COMMENT_COLLECTION,
  COMMENT_REPLY_COLLECTION,
  USER_RELATION_COLLECTION,
  SPACE_RELATION_COLLECTION,
];
// The space type whose role-holders are "everyone in the org" — a
// spaceRelation naming one of these as its subject is an org-wide grant.
const MEMBERS_SPACE_TYPE = "org.atgrp.members";

// SinkDeps is what the sink needs from the syncer driving it: reading a
// record's blob back out of its space (see sync.ts, which wires this to the
// same syncer's getBlob).
export interface SinkDeps {
  getBlob(space: string, did: DidString, cid: string): Promise<Uint8Array>;
}

// RecordChange is one record write or delete, however the stream delivered
// it (an Ops change). value is null for a delete.
export interface RecordChange {
  uri: string;
  cid: string;
  value: LexMap | null;
}

// DocSink keeps chalk's D1
// mirror (docs, doc_access, doc_org_access, comments, comment_replies) and
// each doc's DocRoom in step with the records synced from doc spaces and
// their comments spaces. It replaces sap's outbox webhook.
//
// Delivery is at-least-once, and every handler is an upsert or a delete by
// the record's own URI, so a replayed event is a no-op. Unlike Drop's sink
// this isn't one D1 statement per event — records also land in DocRoom,
// which no D1 statement can cover — so a failed apply can leave a part of
// it applied; the replay that follows converges on the same state.
export class DocSink {
  constructor(
    private env: Env,
    private deps: SinkDeps,
  ) {}

  // domain is the sink's handlers for the indexer: every collection it
  // mirrors, and a deleted space dropping what was mirrored from it.
  domain(): Domain {
    const put = (event: PutEvent<LexMap>) =>
      Effect.tryPromise(() =>
        this.applyChange({
          uri: event.uri,
          cid: event.cid,
          value: event.record,
        }),
      );
    const del = (event: DelEvent) =>
      Effect.tryPromise(() =>
        this.applyChange({ uri: event.uri, cid: "", value: null }),
      );
    return {
      collections: DOC_COLLECTIONS.map((collection) => ({
        collection,
        handlers: { put, del },
      })),
      spaceDeleted: (event) =>
        Effect.tryPromise(async () => {
          const db = getDb(this.env);
          for (const uri of await mirroredRecordUris(db, `${event.space}/`)) {
            await deleteMirroredRecord(db, uri);
          }
        }),
    };
  }

  // applyChange routes one record change to the handler for its collection.
  // Records chalk doesn't mirror (wrong collection, malformed uri, a crdt
  // record without a blob) are skipped rather than failing the batch: they
  // can never become valid on retry, so failing would wedge the space on
  // them forever. A doc this deployment doesn't know yet isn't skipped: it
  // is indexed (see indexUnknownDoc).
  async applyChange(change: RecordChange): Promise<void> {
    const { env } = this;
    let atUri: AtUri;
    try {
      atUri = new AtUri(change.uri);
    } catch {
      return; // not a uri at all
    }
    const spaceRef = atUri.isSpace ? SpaceRef.for(atUri.href) : undefined;
    const { authorDid, collection } = atUri;
    if (!spaceRef || !authorDid || !collection) return;
    const spaceUri = spaceRef.toString();
    const { uri, value } = change;

    if (collection === USER_RELATION_COLLECTION) {
      await handleUserRelation(env, uri, spaceUri, value);
      return;
    }
    if (collection === SPACE_RELATION_COLLECTION) {
      await handleSpaceRelation(env, uri, spaceUri, value);
      return;
    }
    if (collection === COMMENT_COLLECTION) {
      await handleComment(env, change, spaceUri, authorDid);
      return;
    }
    if (collection === COMMENT_REPLY_COLLECTION) {
      await handleCommentReply(env, uri, spaceUri, authorDid, value);
      return;
    }
    if (collection === MARKDOWN_COLLECTION) {
      await handleMarkdown(env, spaceRef, value);
      return;
    }
    if (collection !== CRDT_COLLECTION) return;
    const blob = value?.blob;
    if (!isBlobRef(blob)) return;
    const doc =
      (await docByUri(getDb(env), spaceUri)) ??
      (await indexUnknownDoc(env, spaceRef));
    let bytes: Uint8Array;
    try {
      bytes = await this.deps.getBlob(
        spaceUri,
        authorDid as DidString,
        getBlobCidString(blob),
      );
    } catch (err) {
      // A blob its repo no longer has can never be read on retry, and the
      // stream retries a failing record forever, so skipping it keeps one
      // lost blob from holding back every later record in the group.
      if (isNotFound(err)) {
        console.warn("[sink] crdt blob is gone; skipping", uri, err);
        return;
      }
      throw err;
    }
    const stub = env.DOC.get(env.DOC.idFromName(spaceUri));
    await stub.applyRemote({ spaceUri, ownerDid: doc.ownerDid }, bytes);
  }
}

// isNotFound reports whether a blob read failed because the host answered
// 404 (the syncer's XrpcError and our HabitatError both carry the status).
function isNotFound(err: unknown): boolean {
  return (err as { status?: unknown } | null)?.status === 404;
}

// indexUnknownDoc adds a docs row for a doc this deployment has never seen —
// e.g. one a sign-in's crawl watched after chalk's DB was reset, since only
// createDoc and DocRoom's republish otherwise write that table. The owner is
// the subject of the space's "owner" userRelation, looked up as the space
// authority (the identity whose session the syncer itself reads the space
// with — see sync.ts) since no member is signed in here. When that lookup
// can't answer (no session for the authority, no owner relation) the owner
// falls back to the space's authority DID — what createDoc records for an
// org doc — rather than throwing, since a throw would fail the batch and
// wedge the space on a record that may never become deliverable. The
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
    const { relations } = await createHabitatClient(
      env,
      spaceRef.spaceDid,
    ).call<{
      relations: { subject: string }[];
    }>("network.habitat.relationship.listRelations", "GET", {
      space: spaceUri,
      subjectType: "user",
      relation: "owner",
    });
    ownerDid = relations[0]?.subject ?? ownerDid;
  } catch (err) {
    console.warn("[sink] look up owner of unknown doc", spaceUri, err);
  }
  const doc = { spaceUri, docId: spaceUri, ownerDid, title };
  await upsertDoc(getDb(env), doc);
  return { docId: doc.docId, uri: spaceUri, ownerDid, title: doc.title };
}

// handleMarkdown takes a doc's title from its network.habitat.docs.markdown
// record — the rendered title/content the owner republish writes (see
// DocRoom.republishCanonical) — so a doc synced in from its space is listed under
// its real title instead of "Untitled", without waiting on (or being able to
// perform) a republish of its own. A doc this deployment hasn't seen yet is
// indexed here, since its markdown record can arrive before its crdt record.
// A delete (null) or a record without a title changes nothing.
async function handleMarkdown(
  env: Env,
  spaceRef: SpaceRef,
  value: LexMap | null,
): Promise<void> {
  const title = value?.title;
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
// null value means the record was deleted. A delete carries no subject, only
// the record's own URI, which is why doc_access keeps that URI as a lookup
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
// wrong delete removes the row and the right one matches nothing. No
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
  value: LexMap | null,
): Promise<void> {
  const db = getDb(env);
  if (value === null) {
    await deleteDocAccess(db, uri);
    return;
  }
  const record = relationFields(value);
  if (!record) return;
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
// does for access grants: a live record is an upsert, a null value is a
// delete.
//
// The synced change carries the record's CID, which the comments table
// keeps so a reply can build the com.atproto.repo.strongRef it needs to
// reference this comment.
//
// The record is checked against its lexicon (the generated validateMain).
// The syncer hands records over already decoded from CBOR, so its anchors
// are Uint8Arrays — the form the bytes validator, and the comments table,
// expect. Anything that fails validation isn't a comment chalk can render,
// and is skipped like any other record chalk doesn't mirror.
//
// The author is the repo the record lives in, not a field on the record:
// a comment record can only be written into its own author's repo, so the
// URI is the authoritative claim about who wrote it. A `author` field in
// the record body would be a self-asserted one.
async function handleComment(
  env: Env,
  { uri, cid, value }: RecordChange,
  spaceUri: string,
  repo: string,
): Promise<void> {
  const db = getDb(env);
  if (value === null) {
    await deleteComment(db, uri);
    return;
  }
  const resolved = await docForComments(db, spaceUri);
  if (!resolved) return;
  const { docSpaceUri } = resolved;

  const validated = network.habitat.docs.comment.$safeValidate(value, LENIENT);
  if (!validated.success) return;
  const record = validated.value;

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
// target of a strongRef, so this doesn't need its cid.
// Validated against its lexicon the same way handleComment is; the
// strongRef's uri is a space record URI
// (at://<did>/space/<type>/<skey>/<repo>/<collection>/<rkey>), which is why
// @atproto/lexicon is pinned to the spaces alpha alongside @atproto/syntax.
async function handleCommentReply(
  env: Env,
  uri: string,
  spaceUri: string,
  repo: string,
  value: LexMap | null,
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
    value,
    LENIENT,
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
// ignored here. As with userRelation, a null value is a delete and carries
// only the record's own URI.
async function handleSpaceRelation(
  env: Env,
  uri: string,
  spaceUri: string,
  value: LexMap | null,
): Promise<void> {
  const db = getDb(env);
  if (value === null) {
    await deleteDocOrgAccess(db, uri);
    return;
  }
  const record = relationFields(value);
  if (!record) return;
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

// relationFields reads the two fields chalk uses off a userRelation or
// spaceRelation record, or undefined if either is missing.
function relationFields(
  value: LexMap,
): { subject: string; relation: string } | undefined {
  const { subject, relation } = value as Record<string, LexValue>;
  if (typeof subject !== "string" || typeof relation !== "string") {
    return undefined;
  }
  if (!subject || !relation) return undefined;
  return { subject, relation };
}

// docsDomain is the doc mirror's handlers for the app indexer.
export function docsDomain(env: Env, deps: SinkDeps): Domain {
  return new DocSink(env, deps).domain();
}
