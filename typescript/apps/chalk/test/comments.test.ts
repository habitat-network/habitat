import { env } from "cloudflare:test";
import { beforeEach, expect, it } from "vitest";
import {
  commentByUri,
  commentsForDoc,
  deleteComment,
  deleteCommentReply,
  getDb,
  repliesForDoc,
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
});

it("returns a doc's comments oldest first", async () => {
  const db = getDb(env);
  await upsertComment(db, {
    uri: commentUri(ALICE, "2"),
    cid: "cid2",
    docSpaceUri: DOC,
    authorDid: ALICE,
    body: "second",
    anchorStart: new Uint8Array([1]),
    anchorEnd: new Uint8Array([2]),
    createdAt: 2000,
  });
  await upsertComment(db, {
    uri: commentUri(ALICE, "1"),
    cid: "cid1",
    docSpaceUri: DOC,
    authorDid: ALICE,
    body: "first",
    anchorStart: new Uint8Array([1]),
    anchorEnd: new Uint8Array([2]),
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
    anchorStart: new Uint8Array([1]),
    anchorEnd: new Uint8Array([2]),
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
    anchorStart: new Uint8Array([1]),
    anchorEnd: new Uint8Array([2]),
  });
  await upsertComment(db, {
    uri,
    cid: "cid2",
    docSpaceUri: DOC,
    authorDid: ALICE,
    body: "edited",
    anchorStart: new Uint8Array([1]),
    anchorEnd: new Uint8Array([2]),
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
    anchorStart: new Uint8Array([1]),
    anchorEnd: new Uint8Array([2]),
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
    anchorStart: new Uint8Array([1]),
    anchorEnd: new Uint8Array([2]),
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
    anchorStart: new Uint8Array([1]),
    anchorEnd: new Uint8Array([2]),
  });
  await upsertComment(db, {
    uri: uri2,
    cid: "cid2",
    docSpaceUri: DOC,
    authorDid: BOB,
    body: "keep me",
    anchorStart: new Uint8Array([1]),
    anchorEnd: new Uint8Array([2]),
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
