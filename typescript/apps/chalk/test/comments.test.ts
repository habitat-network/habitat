import { env } from "cloudflare:test";
import { beforeEach, expect, it } from "vitest";
import {
  commentsForDoc,
  commentsInThread,
  deleteComment,
  getDb,
  setThreadResolved,
  upsertComment,
} from "../src/db";

const DOC = "at://did:web:alice.example/space/network.habitat.docs/abc";
const OTHER_DOC = "at://did:web:alice.example/space/network.habitat.docs/xyz";
const ALICE = "did:web:alice.example";
const BOB = "did:web:bob.example";

beforeEach(async () => {
  await env.DB.exec("DELETE FROM comments");
});

it("returns a doc's comments oldest first", async () => {
  const db = getDb(env);
  await upsertComment(db, {
    uri: `${DOC}-comments/${ALICE}/network.habitat.docs.comment/2`,
    docSpaceUri: DOC,
    threadId: "t1",
    authorDid: ALICE,
    body: "second",
    createdAt: 2000,
  });
  await upsertComment(db, {
    uri: `${DOC}-comments/${ALICE}/network.habitat.docs.comment/1`,
    docSpaceUri: DOC,
    threadId: "t1",
    authorDid: ALICE,
    body: "first",
    createdAt: 1000,
  });
  const rows = await commentsForDoc(db, DOC);
  expect(rows.map((r) => r.body)).toEqual(["first", "second"]);
});

it("excludes comments on other docs", async () => {
  const db = getDb(env);
  await upsertComment(db, {
    uri: `${OTHER_DOC}-comments/${ALICE}/network.habitat.docs.comment/1`,
    docSpaceUri: OTHER_DOC,
    threadId: "t1",
    authorDid: ALICE,
    body: "elsewhere",
  });
  expect(await commentsForDoc(db, DOC)).toEqual([]);
});

it("upserts on conflict (same uri) rather than duplicating", async () => {
  const db = getDb(env);
  const uri = `${DOC}-comments/${ALICE}/network.habitat.docs.comment/1`;
  await upsertComment(db, {
    uri,
    docSpaceUri: DOC,
    threadId: "t1",
    authorDid: ALICE,
    body: "original",
  });
  await upsertComment(db, {
    uri,
    docSpaceUri: DOC,
    threadId: "t1",
    authorDid: ALICE,
    body: "edited",
  });
  const rows = await commentsForDoc(db, DOC);
  expect(rows).toHaveLength(1);
  expect(rows[0].body).toBe("edited");
});

it("defaults quotedText to null and resolved to false", async () => {
  const db = getDb(env);
  await upsertComment(db, {
    uri: `${DOC}-comments/${ALICE}/network.habitat.docs.comment/1`,
    docSpaceUri: DOC,
    threadId: "t1",
    authorDid: ALICE,
    body: "hi",
  });
  const [row] = await commentsForDoc(db, DOC);
  expect(row.quotedText).toBeNull();
  expect(row.resolved).toBe(false);
});

it("deleteComment removes only the targeted row", async () => {
  const db = getDb(env);
  const uri1 = `${DOC}-comments/${ALICE}/network.habitat.docs.comment/1`;
  const uri2 = `${DOC}-comments/${BOB}/network.habitat.docs.comment/1`;
  await upsertComment(db, {
    uri: uri1,
    docSpaceUri: DOC,
    threadId: "t1",
    authorDid: ALICE,
    body: "keep me? no",
  });
  await upsertComment(db, {
    uri: uri2,
    docSpaceUri: DOC,
    threadId: "t1",
    authorDid: BOB,
    body: "keep me",
  });
  await deleteComment(db, uri1);
  const rows = await commentsForDoc(db, DOC);
  expect(rows).toHaveLength(1);
  expect(rows[0].uri).toBe(uri2);
});

it("setThreadResolved flips every comment in the thread, and only that thread", async () => {
  const db = getDb(env);
  await upsertComment(db, {
    uri: `${DOC}-comments/${ALICE}/network.habitat.docs.comment/1`,
    docSpaceUri: DOC,
    threadId: "t1",
    authorDid: ALICE,
    body: "first",
  });
  await upsertComment(db, {
    uri: `${DOC}-comments/${BOB}/network.habitat.docs.comment/1`,
    docSpaceUri: DOC,
    threadId: "t1",
    authorDid: BOB,
    body: "reply",
  });
  await upsertComment(db, {
    uri: `${DOC}-comments/${ALICE}/network.habitat.docs.comment/2`,
    docSpaceUri: DOC,
    threadId: "t2",
    authorDid: ALICE,
    body: "unrelated thread",
  });

  await setThreadResolved(db, DOC, "t1", true);

  const t1 = await commentsInThread(db, DOC, "t1");
  expect(t1.every((c) => c.resolved)).toBe(true);
  const t2 = await commentsInThread(db, DOC, "t2");
  expect(t2.every((c) => !c.resolved)).toBe(true);
});
