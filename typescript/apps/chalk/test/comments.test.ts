import { env } from "cloudflare:test";
import { beforeEach, expect, it } from "vitest";
import {
  applyResolution,
  commentByUri,
  commentsForDoc,
  commentsForDocWithResolution,
  deleteComment,
  deleteCommentReply,
  getDb,
  repliesForDoc,
  resolutionsForDoc,
  upsertComment,
  upsertCommentReply,
} from "../src/db";

const DOC = "at://did:web:alice.example/space/network.habitat.docs/abc";
const OTHER_DOC = "at://did:web:alice.example/space/network.habitat.docs/xyz";
const ALICE = "did:web:alice.example";
const BOB = "did:web:bob.example";

function commentUri(author: string, rkey = "1") {
  return `${DOC}-comments/${author}/network.habitat.docs.comment/${rkey}`;
}

beforeEach(async () => {
  await env.DB.exec("DELETE FROM comments");
  await env.DB.exec("DELETE FROM comment_replies");
  await env.DB.exec("DELETE FROM comment_resolutions");
});

it("returns a doc's comments oldest first", async () => {
  const db = getDb(env);
  await upsertComment(db, {
    uri: commentUri(ALICE, "2"),
    cid: "cid2",
    docSpaceUri: DOC,
    authorDid: ALICE,
    body: "second",
    anchorStart: "a",
    anchorEnd: "b",
    createdAt: 2000,
  });
  await upsertComment(db, {
    uri: commentUri(ALICE, "1"),
    cid: "cid1",
    docSpaceUri: DOC,
    authorDid: ALICE,
    body: "first",
    anchorStart: "a",
    anchorEnd: "b",
    createdAt: 1000,
  });
  const rows = await commentsForDoc(db, DOC);
  expect(rows.map((r) => r.body)).toEqual(["first", "second"]);
});

it("excludes comments on other docs", async () => {
  const db = getDb(env);
  await upsertComment(db, {
    uri: `${OTHER_DOC}-comments/${ALICE}/network.habitat.docs.comment/1`,
    cid: "cid1",
    docSpaceUri: OTHER_DOC,
    authorDid: ALICE,
    body: "elsewhere",
    anchorStart: "a",
    anchorEnd: "b",
  });
  expect(await commentsForDoc(db, DOC)).toEqual([]);
});

it("upserts on conflict (same uri) rather than duplicating", async () => {
  const db = getDb(env);
  const uri = commentUri(ALICE);
  await upsertComment(db, {
    uri,
    cid: "cid1",
    docSpaceUri: DOC,
    authorDid: ALICE,
    body: "original",
    anchorStart: "a",
    anchorEnd: "b",
  });
  await upsertComment(db, {
    uri,
    cid: "cid2",
    docSpaceUri: DOC,
    authorDid: ALICE,
    body: "edited",
    anchorStart: "a",
    anchorEnd: "b",
  });
  const rows = await commentsForDoc(db, DOC);
  expect(rows).toHaveLength(1);
  expect(rows[0].body).toBe("edited");
  expect(rows[0].cid).toBe("cid2");
});

it("defaults quotedText to null", async () => {
  const db = getDb(env);
  await upsertComment(db, {
    uri: commentUri(ALICE),
    cid: "cid1",
    docSpaceUri: DOC,
    authorDid: ALICE,
    body: "hi",
    anchorStart: "a",
    anchorEnd: "b",
  });
  const [row] = await commentsForDoc(db, DOC);
  expect(row.quotedText).toBeNull();
});

it("commentByUri looks up a single comment by its uri", async () => {
  const db = getDb(env);
  const uri = commentUri(ALICE);
  await upsertComment(db, {
    uri,
    cid: "cid1",
    docSpaceUri: DOC,
    authorDid: ALICE,
    body: "hi",
    anchorStart: "a",
    anchorEnd: "b",
  });
  expect((await commentByUri(db, uri))?.body).toBe("hi");
  expect(await commentByUri(db, commentUri(BOB))).toBeUndefined();
});

it("deleteComment removes only the targeted row", async () => {
  const db = getDb(env);
  const uri1 = commentUri(ALICE);
  const uri2 = commentUri(BOB);
  await upsertComment(db, {
    uri: uri1,
    cid: "cid1",
    docSpaceUri: DOC,
    authorDid: ALICE,
    body: "keep me? no",
    anchorStart: "a",
    anchorEnd: "b",
  });
  await upsertComment(db, {
    uri: uri2,
    cid: "cid2",
    docSpaceUri: DOC,
    authorDid: BOB,
    body: "keep me",
    anchorStart: "a",
    anchorEnd: "b",
  });
  await deleteComment(db, uri1);
  const rows = await commentsForDoc(db, DOC);
  expect(rows).toHaveLength(1);
  expect(rows[0].uri).toBe(uri2);
});

// commentReplies mirrors network.habitat.docs.commentReply — a reply
// referencing its thread's root comment by strongRef (commentUri) rather
// than carrying an anchor of its own.
it("repliesForDoc returns a doc's replies oldest first, grouped by commentUri client-side", async () => {
  const db = getDb(env);
  const root = commentUri(ALICE);
  await upsertCommentReply(db, {
    uri: `${root}/reply/2`,
    docSpaceUri: DOC,
    commentUri: root,
    authorDid: BOB,
    body: "second reply",
    createdAt: 2000,
  });
  await upsertCommentReply(db, {
    uri: `${root}/reply/1`,
    docSpaceUri: DOC,
    commentUri: root,
    authorDid: ALICE,
    body: "first reply",
    createdAt: 1000,
  });
  const rows = await repliesForDoc(db, DOC);
  expect(rows.map((r) => r.body)).toEqual(["first reply", "second reply"]);
});

it("deleteCommentReply removes only the targeted reply", async () => {
  const db = getDb(env);
  const root = commentUri(ALICE);
  const uri1 = `${root}/reply/1`;
  const uri2 = `${root}/reply/2`;
  await upsertCommentReply(db, {
    uri: uri1,
    docSpaceUri: DOC,
    commentUri: root,
    authorDid: BOB,
    body: "keep me? no",
  });
  await upsertCommentReply(db, {
    uri: uri2,
    docSpaceUri: DOC,
    commentUri: root,
    authorDid: BOB,
    body: "keep me",
  });
  await deleteCommentReply(db, uri1);
  const rows = await repliesForDoc(db, DOC);
  expect(rows).toHaveLength(1);
  expect(rows[0].uri).toBe(uri2);
});

// applyResolution/resolutionsForDoc back network.habitat.docs
// .commentResolution — a resolve/reopen *action* record, referencing its
// thread's root comment by strongRef (commentUri) and written by whoever
// performed it (not necessarily the root's author). See that lexicon's
// comment for why this is a separate append-only log rather than a field
// on the comment record.
it("applyResolution records a resolve action, resolutionsForDoc reflects it", async () => {
  const db = getDb(env);
  const root = commentUri(ALICE);
  await applyResolution(db, {
    docSpaceUri: DOC,
    commentUri: root,
    uri: `${root}-resolution/1`,
    resolverDid: BOB,
    resolved: true,
    createdAt: 1000,
  });
  const rows = await resolutionsForDoc(db, DOC);
  expect(rows).toEqual([
    {
      docSpaceUri: DOC,
      commentUri: root,
      uri: `${root}-resolution/1`,
      resolverDid: BOB,
      resolved: true,
      createdAt: 1000,
    },
  ]);
});

it("a resolver need not be the thread's root author", async () => {
  const db = getDb(env);
  const root = commentUri(ALICE);
  // Alice wrote the root comment...
  await upsertComment(db, {
    uri: root,
    cid: "cid1",
    docSpaceUri: DOC,
    authorDid: ALICE,
    body: "hello",
    anchorStart: "a",
    anchorEnd: "b",
  });
  // ...but Bob is the one who resolves it.
  await applyResolution(db, {
    docSpaceUri: DOC,
    commentUri: root,
    uri: `${root}-resolution/1`,
    resolverDid: BOB,
    resolved: true,
    createdAt: 1000,
  });
  const [row] = await resolutionsForDoc(db, DOC);
  expect(row.resolverDid).toBe(BOB);
  expect(row.resolved).toBe(true);
});

it("a later action (by createdAt) overwrites an earlier one, regardless of who wrote it", async () => {
  const db = getDb(env);
  const root = commentUri(ALICE);
  await applyResolution(db, {
    docSpaceUri: DOC,
    commentUri: root,
    uri: `${root}-resolution/1`,
    resolverDid: ALICE,
    resolved: true,
    createdAt: 1000,
  });
  await applyResolution(db, {
    docSpaceUri: DOC,
    commentUri: root,
    uri: `${root}-resolution/2`,
    resolverDid: BOB,
    resolved: false,
    createdAt: 2000,
  });
  const rows = await resolutionsForDoc(db, DOC);
  expect(rows).toHaveLength(1);
  expect(rows[0]).toMatchObject({ resolverDid: BOB, resolved: false });
});

it("a stale action (older createdAt) arriving after a newer one is a no-op", async () => {
  const db = getDb(env);
  const root = commentUri(ALICE);
  await applyResolution(db, {
    docSpaceUri: DOC,
    commentUri: root,
    uri: `${root}-resolution/1`,
    resolverDid: BOB,
    resolved: false,
    createdAt: 2000,
  });
  // An older action delivered late (e.g. redelivered by the outbox) must
  // not clobber the newer state.
  await applyResolution(db, {
    docSpaceUri: DOC,
    commentUri: root,
    uri: `${root}-resolution/2`,
    resolverDid: ALICE,
    resolved: true,
    createdAt: 1000,
  });
  const rows = await resolutionsForDoc(db, DOC);
  expect(rows).toHaveLength(1);
  expect(rows[0]).toMatchObject({ resolverDid: BOB, resolved: false });
});

it("resolutionsForDoc only returns resolutions for the given doc", async () => {
  const db = getDb(env);
  const otherRoot = `${OTHER_DOC}-comments/${ALICE}/network.habitat.docs.comment/1`;
  await applyResolution(db, {
    docSpaceUri: OTHER_DOC,
    commentUri: otherRoot,
    uri: `${otherRoot}-resolution/1`,
    resolverDid: ALICE,
    resolved: true,
    createdAt: 1000,
  });
  expect(await resolutionsForDoc(db, DOC)).toEqual([]);
});

it("a thread with no resolution action is simply absent from the result", async () => {
  const db = getDb(env);
  await upsertComment(db, {
    uri: commentUri(ALICE),
    cid: "cid1",
    docSpaceUri: DOC,
    authorDid: ALICE,
    body: "hello",
    anchorStart: "a",
    anchorEnd: "b",
  });
  expect(await resolutionsForDoc(db, DOC)).toEqual([]);
});

// commentsForDocWithResolution left-joins comment_resolutions in at the SQL
// level rather than the caller fetching resolutions separately and
// matching them up itself.
it("commentsForDocWithResolution reports resolved: false for a thread with no resolution action", async () => {
  const db = getDb(env);
  const uri = commentUri(ALICE);
  await upsertComment(db, {
    uri,
    cid: "cid1",
    docSpaceUri: DOC,
    authorDid: ALICE,
    body: "hello",
    anchorStart: "a",
    anchorEnd: "b",
  });
  const [row] = await commentsForDocWithResolution(db, DOC);
  expect(row).toMatchObject({ uri, body: "hello", resolved: false });
});

it("commentsForDocWithResolution reflects a resolve action taken by someone other than the author", async () => {
  const db = getDb(env);
  const uri = commentUri(ALICE);
  await upsertComment(db, {
    uri,
    cid: "cid1",
    docSpaceUri: DOC,
    authorDid: ALICE,
    body: "hello",
    anchorStart: "a",
    anchorEnd: "b",
  });
  await applyResolution(db, {
    docSpaceUri: DOC,
    commentUri: uri,
    uri: `${uri}-resolution/1`,
    resolverDid: BOB,
    resolved: true,
    createdAt: 1000,
  });
  const [row] = await commentsForDocWithResolution(db, DOC);
  expect(row.resolved).toBe(true);
});

it("commentsForDocWithResolution tracks a reopen (the latest action wins)", async () => {
  const db = getDb(env);
  const uri = commentUri(ALICE);
  await upsertComment(db, {
    uri,
    cid: "cid1",
    docSpaceUri: DOC,
    authorDid: ALICE,
    body: "hello",
    anchorStart: "a",
    anchorEnd: "b",
  });
  await applyResolution(db, {
    docSpaceUri: DOC,
    commentUri: uri,
    uri: `${uri}-resolution/1`,
    resolverDid: BOB,
    resolved: true,
    createdAt: 1000,
  });
  await applyResolution(db, {
    docSpaceUri: DOC,
    commentUri: uri,
    uri: `${uri}-resolution/2`,
    resolverDid: BOB,
    resolved: false,
    createdAt: 2000,
  });
  const [row] = await commentsForDocWithResolution(db, DOC);
  expect(row.resolved).toBe(false);
});

it("commentsForDocWithResolution keeps each thread's own resolution separate", async () => {
  const db = getDb(env);
  const resolvedUri = commentUri(ALICE, "1");
  const unresolvedUri = commentUri(ALICE, "2");
  await upsertComment(db, {
    uri: resolvedUri,
    cid: "cid1",
    docSpaceUri: DOC,
    authorDid: ALICE,
    body: "resolved thread",
    anchorStart: "a",
    anchorEnd: "b",
    createdAt: 1000,
  });
  await upsertComment(db, {
    uri: unresolvedUri,
    cid: "cid2",
    docSpaceUri: DOC,
    authorDid: ALICE,
    body: "unresolved thread",
    anchorStart: "a",
    anchorEnd: "b",
    createdAt: 2000,
  });
  await applyResolution(db, {
    docSpaceUri: DOC,
    commentUri: resolvedUri,
    uri: `${resolvedUri}-resolution/1`,
    resolverDid: BOB,
    resolved: true,
    createdAt: 1500,
  });
  const rows = await commentsForDocWithResolution(db, DOC);
  expect(rows).toEqual([
    expect.objectContaining({ uri: resolvedUri, resolved: true }),
    expect.objectContaining({ uri: unresolvedUri, resolved: false }),
  ]);
});
