import { env } from "cloudflare:test";
import { beforeEach, expect, it, vi } from "vitest";
import * as Y from "yjs";
import { processOutboxMessage } from "../src/server/outbox";
import {
  commentsForDoc,
  docsFor,
  getDb,
  repliesForDoc,
  upsertDoc,
} from "../src/db";

// A well-formed empty Yjs V2 update — `applyRemote` feeds getBlob's response
// straight into `mergeUpdate`, which decodes it, so an arbitrary byte
// sequence like [0, 0] throws "Unexpected end of array" instead of
// exercising the routing behavior this test is actually about.
const EMPTY_UPDATE = Y.encodeStateAsUpdateV2(new Y.Doc());

const OWNER = "did:web:alice.example";
const URI = `at://${OWNER}/space/network.habitat.docs/abc`;
const RECORD = `${URI}/did:web:bob.example/network.habitat.docs.crdt/self`;

const fetchMock = vi.fn();
beforeEach(async () => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
  await env.DB.exec("DELETE FROM docs");
  await env.DB.exec("DELETE FROM doc_access");
  await env.DB.exec("DELETE FROM doc_org_access");
  await env.DB.exec("DELETE FROM comments");
  await env.DB.exec("DELETE FROM comment_replies");
  await upsertDoc(getDb(env), {
    spaceUri: URI,
    docId: URI,
    ownerDid: OWNER,
    title: "Untitled",
  });
});

function msg(uri: string, cid: string | undefined) {
  return { id: 1, uri, value: cid ? { blob: { ref: { $link: cid } } } : {} };
}

it("routes a crdt record to its doc room", async () => {
  // A fresh Response per call, not a shared instance: this Response's body
  // ends up read inside DocRoom (via applyRemote -> getBlob), a different
  // Durable Object than this test's own execution context — reusing an
  // instance created here hits a real Workers I/O-ownership restriction
  // ("Cannot perform I/O on behalf of a different Durable Object").
  fetchMock.mockImplementation(async () => new Response(EMPTY_UPDATE));
  await processOutboxMessage(env, msg(RECORD, "cid1"));
  expect(
    fetchMock.mock.calls.some((c) => String(c[0]).includes("space.getBlob")),
  ).toBe(true);
});

it("ignores a record in a different collection", async () => {
  const other = `${URI}/did:web:bob.example/network.habitat.docs.markdown/self`;
  await processOutboxMessage(env, msg(other, "cid1"));
  expect(fetchMock).not.toHaveBeenCalled();
});

it("ignores a doc absent from the index", async () => {
  const unknown = `at://${OWNER}/space/network.habitat.docs/zzz/did:web:bob.example/network.habitat.docs.crdt/self`;
  await processOutboxMessage(env, msg(unknown, "cid1"));
  expect(fetchMock).not.toHaveBeenCalled();
});

it("ignores a record with no blob reference", async () => {
  await processOutboxMessage(env, msg(RECORD, undefined));
  expect(fetchMock).not.toHaveBeenCalled();
});

it("throws on a malformed uri", async () => {
  // Unlike the "ignores" cases above (a well-formed uri this deliberately
  // doesn't care about), a uri that isn't a uri at all can't be
  // distinguished from a bug — this throws so handleSapWebhook (webhook.ts)
  // turns it into a 500 and sap retries, instead of silently acking a
  // message that was never actually processed.
  await expect(
    processOutboxMessage(env, msg("not-a-uri", "cid1")),
  ).rejects.toThrow();
  expect(fetchMock).not.toHaveBeenCalled();
});

const BOB = "did:web:bob.example";
const RELATION_RECORD = `${URI}/${OWNER}/network.habitat.relationship.userRelation/rkey1`;

function relationMsg(uri: string, value: unknown) {
  return { id: 1, uri, value };
}

it("records a doc_access grant from a userRelation record", async () => {
  await processOutboxMessage(
    env,
    relationMsg(RELATION_RECORD, { subject: BOB, relation: "writer" }),
  );
  const rows = await docsFor(getDb(env), BOB);
  expect(rows).toEqual([
    { docId: URI, uri: URI, ownerDid: OWNER, title: "Untitled" },
  ]);
});

it("removes the grant on a delete tombstone (null value)", async () => {
  await processOutboxMessage(
    env,
    relationMsg(RELATION_RECORD, { subject: BOB, relation: "writer" }),
  );
  await processOutboxMessage(env, relationMsg(RELATION_RECORD, null));
  expect(await docsFor(getDb(env), BOB)).toEqual([]);
});

it("re-granting the same record uri updates rather than duplicates", async () => {
  await processOutboxMessage(
    env,
    relationMsg(RELATION_RECORD, { subject: BOB, relation: "writer" }),
  );
  await processOutboxMessage(
    env,
    relationMsg(RELATION_RECORD, { subject: BOB, relation: "reader" }),
  );
  expect(await docsFor(getDb(env), BOB)).toHaveLength(1);
});

it("ignores a userRelation record missing subject or relation", async () => {
  await processOutboxMessage(
    env,
    relationMsg(RELATION_RECORD, { subject: BOB }),
  );
  expect(await docsFor(getDb(env), BOB)).toEqual([]);
});

const ORG = "did:web:org.example";
const ORG_DOC = `at://${ORG}/space/network.habitat.docs/org1`;
const MEMBERS_SPACE = `at://${ORG}/space/community.opensocial.members/self`;
const SPACE_RELATION_RECORD = `${ORG_DOC}/${ORG}/network.habitat.relationship.spaceRelation/rkey1`;
const ORG_DOC_SUMMARY = {
  docId: ORG_DOC,
  uri: ORG_DOC,
  ownerDid: ORG,
  title: "Org doc",
};

async function seedOrgDoc() {
  await upsertDoc(getDb(env), {
    spaceUri: ORG_DOC,
    docId: ORG_DOC,
    ownerDid: ORG,
    title: "Org doc",
  });
}

it("records an org-wide grant from a members-space spaceRelation", async () => {
  await seedOrgDoc();
  await processOutboxMessage(
    env,
    relationMsg(SPACE_RELATION_RECORD, {
      subject: MEMBERS_SPACE,
      subjectRole: "reader",
      relation: "reader",
    }),
  );
  // BOB holds no personal grant — the org-wide row is what surfaces it.
  expect(await docsFor(getDb(env), BOB, ORG)).toEqual([ORG_DOC_SUMMARY]);
});

it("removes the org-wide grant on a delete tombstone", async () => {
  await seedOrgDoc();
  await processOutboxMessage(
    env,
    relationMsg(SPACE_RELATION_RECORD, {
      subject: MEMBERS_SPACE,
      subjectRole: "reader",
      relation: "reader",
    }),
  );
  await processOutboxMessage(env, relationMsg(SPACE_RELATION_RECORD, null));
  expect(await docsFor(getDb(env), BOB, ORG)).toEqual([]);
});

it("ignores a spaceRelation whose subject is not a members space", async () => {
  await seedOrgDoc();
  await processOutboxMessage(
    env,
    relationMsg(SPACE_RELATION_RECORD, {
      subject: `at://${ORG}/space/network.habitat.group/some-group`,
      subjectRole: "writer",
      relation: "reader",
    }),
  );
  expect(await docsFor(getDb(env), BOB, ORG)).toEqual([]);
});

// A commenter's grant is a userRelation on the doc's *comments* space (see
// functions.ts's ROLE_TO_GRANT). doc_access answers "which docs can this
// subject see", and docsFor joins it against the doc — so the row
// has to be filed under the doc space, not the space the record names, or
// the commenter never sees the doc in their own list.
const COMMENTS_RELATION_RECORD = `at://${OWNER}/space/network.habitat.docs.comments/abc/${OWNER}/network.habitat.relationship.userRelation/rkey2`;

it("files a comments-space grant under the doc space", async () => {
  await processOutboxMessage(
    env,
    relationMsg(COMMENTS_RELATION_RECORD, { subject: BOB, relation: "writer" }),
  );
  expect(await docsFor(getDb(env), BOB)).toEqual([
    { docId: URI, uri: URI, ownerDid: OWNER, title: "Untitled" },
  ]);
});

it("a non-writer relation on the comments space leaves the doc-space row alone", async () => {
  // Creating the comments space writes its creator an *owner* userRelation
  // on it, alongside the one they already hold on the doc space. Both
  // would map to the same (subject, doc space) row, and the second to
  // arrive would overwrite the first's record URI — after which the wrong
  // tombstone deletes the row and the right one matches nothing. Only a
  // writer grant on the comments space means anything to doc_access.
  await processOutboxMessage(
    env,
    relationMsg(RELATION_RECORD, { subject: BOB, relation: "owner" }),
  );
  await processOutboxMessage(
    env,
    relationMsg(COMMENTS_RELATION_RECORD, { subject: BOB, relation: "owner" }),
  );
  // The doc-space record's own tombstone must still find the row.
  await processOutboxMessage(env, relationMsg(RELATION_RECORD, null));
  expect(await docsFor(getDb(env), BOB)).toEqual([]);
});

it("removes a comments-space grant on its delete tombstone", async () => {
  await processOutboxMessage(
    env,
    relationMsg(COMMENTS_RELATION_RECORD, { subject: BOB, relation: "writer" }),
  );
  // The tombstone carries only the record's own URI — on the comments
  // space — which still has to find the row filed under the doc space.
  await processOutboxMessage(env, relationMsg(COMMENTS_RELATION_RECORD, null));
  expect(await docsFor(getDb(env), BOB)).toEqual([]);
});

const COMMENTS_SPACE = `at://${OWNER}/space/network.habitat.docs.comments/abc`;
const COMMENT_RECORD = `${COMMENTS_SPACE}/${BOB}/network.habitat.docs.comment/3jzfcijpj2z2a`;

function commentMsg(uri: string, value: unknown) {
  return { id: 1, uri, value };
}

// handleComment backfills the comment's cid via a getRecord call
// (authenticated as the doc's owner) since the outbox message itself
// carries none — see outbox.ts's handleComment. mockGetRecord makes
// fetchMock answer that call; other calls (there shouldn't be any in
// these tests) fail loudly instead of hanging.
function mockGetRecord(cid: string) {
  fetchMock.mockImplementation(async (url: string) => {
    if (String(url).includes("space.getRecord")) {
      return new Response(JSON.stringify({ uri: COMMENT_RECORD, cid }), {
        status: 200,
      });
    }
    throw new Error(`unexpected fetch: ${url}`);
  });
}

it("mirrors a comment record into the comments table, backfilling its cid via getRecord", async () => {
  mockGetRecord("bafycid1");
  await processOutboxMessage(
    env,
    commentMsg(COMMENT_RECORD, {
      body: "nice doc",
      anchorStart: { $bytes: "c3RhcnQtcmVsLXBvcw" },
      anchorEnd: { $bytes: "ZW5kLXJlbC1wb3M" },
      createdAt: "2024-01-01T00:00:00.000Z",
    }),
  );
  const rows = await commentsForDoc(getDb(env), URI);
  expect(rows).toHaveLength(1);
  expect(rows[0]).toMatchObject({
    uri: COMMENT_RECORD,
    cid: "bafycid1",
    docSpaceUri: URI,
    authorDid: BOB, // the repo holding the record, not a field on it
    body: "nice doc",
    anchorStart: "c3RhcnQtcmVsLXBvcw",
    anchorEnd: "ZW5kLXJlbC1wb3M",
  });
});

it("removes the comment on a delete tombstone (null value), without calling getRecord", async () => {
  mockGetRecord("bafycid1");
  await processOutboxMessage(
    env,
    commentMsg(COMMENT_RECORD, {
      body: "nice doc",
      anchorStart: { $bytes: "YQ" },
      anchorEnd: { $bytes: "Yg" },
    }),
  );
  fetchMock.mockReset();
  fetchMock.mockImplementation(async () => {
    throw new Error("should not be called for a tombstone");
  });
  await processOutboxMessage(env, commentMsg(COMMENT_RECORD, null));
  expect(await commentsForDoc(getDb(env), URI)).toEqual([]);
});

it("ignores a comment on a doc this deployment doesn't know", async () => {
  const unknownSpace = `at://${OWNER}/space/network.habitat.docs.comments/zzz`;
  const unknownRecord = `${unknownSpace}/${BOB}/network.habitat.docs.comment/1`;
  await processOutboxMessage(
    env,
    commentMsg(unknownRecord, {
      body: "x",
      anchorStart: { $bytes: "YQ" },
      anchorEnd: { $bytes: "Yg" },
    }),
  );
  expect(fetchMock).not.toHaveBeenCalled(); // never reaches the getRecord call
  expect(
    await commentsForDoc(
      getDb(env),
      `at://${OWNER}/space/network.habitat.docs/zzz`,
    ),
  ).toEqual([]);
});

it("ignores a comment record missing body or an anchor", async () => {
  await processOutboxMessage(env, commentMsg(COMMENT_RECORD, { body: "x" }));
  expect(fetchMock).not.toHaveBeenCalled();
  expect(await commentsForDoc(getDb(env), URI)).toEqual([]);
});

it("drops a comment whose getRecord call fails (can't mirror without a cid)", async () => {
  fetchMock.mockImplementation(
    async () => new Response("nope", { status: 404 }),
  );
  await processOutboxMessage(
    env,
    commentMsg(COMMENT_RECORD, {
      body: "x",
      anchorStart: { $bytes: "YQ" },
      anchorEnd: { $bytes: "Yg" },
    }),
  );
  expect(await commentsForDoc(getDb(env), URI)).toEqual([]);
});

const COMMENT_REPLY_RECORD = `${COMMENTS_SPACE}/${BOB}/network.habitat.docs.commentReply/xyz`;
const ROOT_COMMENT_URI = `${COMMENTS_SPACE}/${OWNER}/network.habitat.docs.comment/1`;

function replyMsg(uri: string, value: unknown) {
  return { id: 1, uri, value };
}

it("mirrors a commentReply record into comment_replies, without needing a cid (no getRecord call)", async () => {
  fetchMock.mockImplementation(async () => {
    throw new Error("replies should not need a getRecord call");
  });
  await processOutboxMessage(
    env,
    replyMsg(COMMENT_REPLY_RECORD, {
      comment: { uri: ROOT_COMMENT_URI, cid: "bafyroot" },
      body: "I agree",
      createdAt: "2024-01-01T00:00:00.000Z",
    }),
  );
  const rows = await repliesForDoc(getDb(env), URI);
  expect(rows).toEqual([
    expect.objectContaining({
      uri: COMMENT_REPLY_RECORD,
      docSpaceUri: URI,
      commentUri: ROOT_COMMENT_URI,
      authorDid: BOB,
      body: "I agree",
    }),
  ]);
});

it("removes the reply on a delete tombstone (null value)", async () => {
  await processOutboxMessage(
    env,
    replyMsg(COMMENT_REPLY_RECORD, {
      comment: { uri: ROOT_COMMENT_URI, cid: "bafyroot" },
      body: "I agree",
    }),
  );
  await processOutboxMessage(env, replyMsg(COMMENT_REPLY_RECORD, null));
  expect(await repliesForDoc(getDb(env), URI)).toEqual([]);
});

it("ignores a commentReply record missing its comment ref or body", async () => {
  await processOutboxMessage(
    env,
    replyMsg(COMMENT_REPLY_RECORD, { body: "no ref" }),
  );
  expect(await repliesForDoc(getDb(env), URI)).toEqual([]);
});
