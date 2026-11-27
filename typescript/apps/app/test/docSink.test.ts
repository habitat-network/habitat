import { env } from "cloudflare:test";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { warmHabitat } from "./warmHabitat";
import * as Y from "yjs";
import { jsonToLex, type LexMap } from "@atproto/lex";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import { delEvent, indexEvents, putEvent, spaceDeletedEvent } from "./events";
import { DocSink } from "../src/server/docs/sink";
import {
  commentsForDoc,
  docByUri,
  docsFor,
  getDb,
  repliesForDoc,
  upsertDoc,
} from "../src/db";

vi.mock("../src/server/oauth", () => import("./fakeOAuth"));

beforeAll(warmHabitat, 60_000);

// A well-formed empty Yjs V2 update — `applyRemote` applies the blob's bytes
// with `Y.applyUpdateV2`, which decodes them, so an arbitrary byte sequence
// like [0, 0] throws "Unexpected end of array" instead of exercising the
// routing behavior this test is actually about.
const EMPTY_UPDATE = Y.encodeStateAsUpdateV2(new Y.Doc());

const OWNER = "did:web:alice.example";
const URI = `at:space/${OWNER}/network.habitat.docs/abc`;
const RECORD = `${URI}/did:web:bob.example/network.habitat.docs.crdt/self`;
const RECORD_CID =
  "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm";
const BLOB_CID = "bafkreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku";

const fetchMock = vi.fn();
// getBlob stands in for the syncer's own getBlob (see sync.ts's newSyncer).
const getBlob = vi.fn(async () => EMPTY_UPDATE);
beforeEach(async () => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
  getBlob.mockClear();
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

// apply hands one record write (or, with a null value, delete) to the sink
// the way the syncer would: values arrive decoded, so the JSON fixtures
// below ($bytes, $link) are turned into their lex form first.
async function apply(uri: string, value: unknown, cid = RECORD_CID) {
  await new DocSink(env, { getBlob }).applyChange({
    uri,
    cid,
    value: value === null ? null : (jsonToLex(value as never) as LexMap),
  });
}

// fakePds answers the owner lookup indexUnknownDoc makes (listRelations, as
// the space authority) via the given callback.
function fakePds(
  listRelations: () => Response = () => Response.json({ relations: [] }),
) {
  return async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes("relationship.listRelations")) return listRelations();
    throw new Error(`unexpected fetch: ${url}`);
  };
}

function crdt(cid: string | undefined) {
  return cid
    ? {
        blob: {
          $type: "blob",
          ref: { $link: cid },
          mimeType: "application/octet-stream",
          size: 1,
        },
      }
    : {};
}

it("routes a crdt record to its doc room, reading the blob through the syncer", async () => {
  await apply(RECORD, crdt(BLOB_CID));
  expect(getBlob).toHaveBeenCalledWith(URI, "did:web:bob.example", BLOB_CID);
});

it("skips a crdt record whose blob the host no longer has", async () => {
  getBlob.mockRejectedValueOnce(
    Object.assign(new Error("Upstream server responded with a 404 error"), {
      status: 404,
    }),
  );
  await expect(apply(RECORD, crdt(BLOB_CID))).resolves.toBeUndefined();
});

it("still fails a crdt record whose blob read fails for another reason", async () => {
  getBlob.mockRejectedValueOnce(new Error("boom"));
  await expect(apply(RECORD, crdt(BLOB_CID))).rejects.toThrow("boom");
});

it("ignores a record in a different collection", async () => {
  const other = `${URI}/did:web:bob.example/network.habitat.docs.comment/c1`;
  await apply(other, crdt(BLOB_CID));
  expect(getBlob).not.toHaveBeenCalled();
});

const UNKNOWN_URI = `at:space/${OWNER}/network.habitat.docs/zzz`;
const UNKNOWN_RECORD = `${UNKNOWN_URI}/did:web:bob.example/network.habitat.docs.crdt/self`;
const CAROL = "did:web:carol.example";

it("indexes a doc absent from the index under its owner relation", async () => {
  fetchMock.mockImplementation(
    fakePds(() =>
      Response.json({
        relations: [
          {
            uri: `${UNKNOWN_URI}/${OWNER}/network.habitat.relationship.userRelation/r1`,
            subject: CAROL,
            relation: "owner",
            object: UNKNOWN_URI,
          },
        ],
      }),
    ),
  );
  await apply(UNKNOWN_RECORD, crdt(BLOB_CID));

  expect(await docByUri(getDb(env), UNKNOWN_URI)).toEqual({
    docId: UNKNOWN_URI,
    uri: UNKNOWN_URI,
    ownerDid: CAROL,
    title: "Untitled",
  });
  const listCall = fetchMock.mock.calls.find((c) =>
    String(c[0]).includes("relationship.listRelations"),
  );
  const params = new URL(String(listCall?.[0])).searchParams;
  expect(params.get("space")).toBe(UNKNOWN_URI);
  expect(params.get("relation")).toBe("owner");
  expect(params.get("subjectType")).toBe("user");
  expect(getBlob).toHaveBeenCalled();
});

it("falls back to the space authority when a doc has no owner relation", async () => {
  fetchMock.mockImplementation(fakePds(() => Response.json({ relations: [] })));
  await apply(UNKNOWN_RECORD, crdt(BLOB_CID));
  expect((await docByUri(getDb(env), UNKNOWN_URI))?.ownerDid).toBe(OWNER);
  expect(getBlob).toHaveBeenCalled();
});

it("falls back to the space authority when the owner lookup fails", async () => {
  fetchMock.mockImplementation(
    fakePds(() => new Response("upstream error", { status: 502 })),
  );
  await apply(UNKNOWN_RECORD, crdt(BLOB_CID));
  expect((await docByUri(getDb(env), UNKNOWN_URI))?.ownerDid).toBe(OWNER);
});

const MARKDOWN_RECORD = `${URI}/${OWNER}/network.habitat.docs.markdown/self`;

it("sets a doc's title from its markdown record", async () => {
  await apply(MARKDOWN_RECORD, {
    title: "Quarterly plan",
    content: "Quarterly plan\n\nDetails",
  });
  expect((await docByUri(getDb(env), URI))?.title).toBe("Quarterly plan");
});

it("indexes a doc absent from the index under its markdown record's title", async () => {
  fetchMock.mockImplementation(
    fakePds(() =>
      Response.json({
        relations: [
          { uri: "r1", subject: CAROL, relation: "owner", object: UNKNOWN_URI },
        ],
      }),
    ),
  );
  await apply(`${UNKNOWN_URI}/${CAROL}/network.habitat.docs.markdown/self`, {
    title: "Launch notes",
    content: "Launch notes",
  });
  expect(await docByUri(getDb(env), UNKNOWN_URI)).toEqual({
    docId: UNKNOWN_URI,
    uri: UNKNOWN_URI,
    ownerDid: CAROL,
    title: "Launch notes",
  });
});

it("ignores a markdown delete", async () => {
  await apply(MARKDOWN_RECORD, null);
  expect((await docByUri(getDb(env), URI))?.title).toBe("Untitled");
});

it("ignores a record with no blob reference", async () => {
  await apply(RECORD, crdt(undefined));
  expect(getBlob).not.toHaveBeenCalled();
});

it("ignores a malformed uri", async () => {
  await apply("not-a-uri", crdt(BLOB_CID));
  expect(getBlob).not.toHaveBeenCalled();
});

const BOB = "did:web:bob.example";
const RELATION_RECORD = `${URI}/${OWNER}/network.habitat.relationship.userRelation/rkey1`;

it("records a doc_access grant from a userRelation record", async () => {
  await apply(RELATION_RECORD, { subject: BOB, relation: "writer" });
  const rows = await docsFor(getDb(env), BOB);
  expect(rows).toEqual([
    { docId: URI, uri: URI, ownerDid: OWNER, title: "Untitled" },
  ]);
});

it("removes the grant on a delete (null value)", async () => {
  await apply(RELATION_RECORD, { subject: BOB, relation: "writer" });
  await apply(RELATION_RECORD, null);
  expect(await docsFor(getDb(env), BOB)).toEqual([]);
});

it("re-granting the same record uri updates rather than duplicates", async () => {
  await apply(RELATION_RECORD, { subject: BOB, relation: "writer" });
  await apply(RELATION_RECORD, { subject: BOB, relation: "reader" });
  expect(await docsFor(getDb(env), BOB)).toHaveLength(1);
});

it("ignores a userRelation record missing subject or relation", async () => {
  await apply(RELATION_RECORD, { subject: BOB });
  expect(await docsFor(getDb(env), BOB)).toEqual([]);
});

const ORG = "did:web:org.example";
const ORG_DOC = `at:space/${ORG}/network.habitat.docs/org1`;
const MEMBERS_SPACE = `at:space/${ORG}/community.opensocial.members/self`;
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
  await apply(SPACE_RELATION_RECORD, {
    subject: MEMBERS_SPACE,
    subjectRole: "reader",
    relation: "reader",
  });
  // BOB holds no personal grant — the org-wide row is what surfaces it.
  expect(await docsFor(getDb(env), BOB, ORG)).toEqual([ORG_DOC_SUMMARY]);
});

it("removes the org-wide grant on a delete", async () => {
  await seedOrgDoc();
  await apply(SPACE_RELATION_RECORD, {
    subject: MEMBERS_SPACE,
    subjectRole: "reader",
    relation: "reader",
  });
  await apply(SPACE_RELATION_RECORD, null);
  expect(await docsFor(getDb(env), BOB, ORG)).toEqual([]);
});

it("keeps canonical candidates inert across legacy grant and revoke replays", async () => {
  await seedOrgDoc();
  const canonicalRecord = `${SPACE_RELATION_RECORD}-canonical`;
  const canonicalGrant = {
    subject: `at:space/${ORG}/org.atgrp.members/self`,
    subjectRole: "reader",
    relation: "writer",
  };
  const legacyGrant = {
    subject: MEMBERS_SPACE,
    subjectRole: "reader",
    relation: "reader",
  };
  const visible = () => docsFor(getDb(env), BOB, ORG);

  await apply(canonicalRecord, canonicalGrant);
  expect(await visible()).toEqual([]);

  await apply(SPACE_RELATION_RECORD, legacyGrant);
  await apply(canonicalRecord, canonicalGrant);
  await apply(canonicalRecord, null);
  expect(await visible()).toEqual([ORG_DOC_SUMMARY]);

  await apply(SPACE_RELATION_RECORD, legacyGrant);
  await apply(canonicalRecord, canonicalGrant);
  await apply(SPACE_RELATION_RECORD, null);
  await apply(canonicalRecord, canonicalGrant);
  expect(await visible()).toEqual([]);
});

it("ignores a spaceRelation whose subject is not a members space", async () => {
  await seedOrgDoc();
  await apply(SPACE_RELATION_RECORD, {
    subject: `at:space/${ORG}/network.habitat.group/some-group`,
    subjectRole: "writer",
    relation: "reader",
  });
  expect(await docsFor(getDb(env), BOB, ORG)).toEqual([]);
});

// A commenter's grant is a userRelation on the doc's *comments* space (see
// functions.ts's ROLE_TO_GRANT). doc_access answers "which docs can this
// subject see", and docsFor joins it against the doc — so the row
// has to be filed under the doc space, not the space the record names, or
// the commenter never sees the doc in their own list.
const COMMENTS_RELATION_RECORD = `at:space/${OWNER}/network.habitat.docs.comments/abc/${OWNER}/network.habitat.relationship.userRelation/rkey2`;

it("files a comments-space grant under the doc space", async () => {
  await apply(COMMENTS_RELATION_RECORD, { subject: BOB, relation: "writer" });
  expect(await docsFor(getDb(env), BOB)).toEqual([
    { docId: URI, uri: URI, ownerDid: OWNER, title: "Untitled" },
  ]);
});

it("a non-writer relation on the comments space leaves the doc-space row alone", async () => {
  // Creating the comments space writes its creator an *owner* userRelation
  // on it, alongside the one they already hold on the doc space. Both
  // would map to the same (subject, doc space) row, and the second to
  // arrive would overwrite the first's record URI — after which the wrong
  // delete removes the row and the right one matches nothing. Only a
  // writer grant on the comments space means anything to doc_access.
  await apply(RELATION_RECORD, { subject: BOB, relation: "owner" });
  await apply(COMMENTS_RELATION_RECORD, { subject: BOB, relation: "owner" });
  // The doc-space record's own delete must still find the row.
  await apply(RELATION_RECORD, null);
  expect(await docsFor(getDb(env), BOB)).toEqual([]);
});

it("removes a comments-space grant on its delete", async () => {
  await apply(COMMENTS_RELATION_RECORD, { subject: BOB, relation: "writer" });
  // The delete carries only the record's own URI — on the comments
  // space — which still has to find the row filed under the doc space.
  await apply(COMMENTS_RELATION_RECORD, null);
  expect(await docsFor(getDb(env), BOB)).toEqual([]);
});

const COMMENTS_SPACE = `at:space/${OWNER}/network.habitat.docs.comments/abc`;
const COMMENT_RECORD = `${COMMENTS_SPACE}/${BOB}/network.habitat.docs.comment/3jzfcijpj2z2a`;

it("mirrors a comment record, with its cid, into the comments table", async () => {
  await apply(COMMENT_RECORD, {
    $type: "network.habitat.docs.comment",
    body: "nice doc",
    anchorStart: { $bytes: "c3RhcnQtcmVsLXBvcw" },
    anchorEnd: { $bytes: "ZW5kLXJlbC1wb3M" },
    createdAt: "2024-01-01T00:00:00.000Z",
  });
  const rows = await commentsForDoc(getDb(env), URI);
  expect(rows).toHaveLength(1);
  expect(rows[0]).toMatchObject({
    uri: COMMENT_RECORD,
    cid: RECORD_CID,
    docSpaceUri: URI,
    authorDid: BOB, // the repo holding the record, not a field on it
    body: "nice doc",
    anchorStart: new TextEncoder().encode("start-rel-pos"),
    anchorEnd: new TextEncoder().encode("end-rel-pos"),
    createdAt: Date.parse("2024-01-01T00:00:00.000Z"),
  });
});

it("removes the comment on a delete (null value)", async () => {
  await apply(COMMENT_RECORD, {
    $type: "network.habitat.docs.comment",
    body: "nice doc",
    anchorStart: { $bytes: "YQ" },
    anchorEnd: { $bytes: "Yg" },
    createdAt: "2024-01-01T00:00:00.000Z",
  });
  expect(await commentsForDoc(getDb(env), URI)).toHaveLength(1);
  await apply(COMMENT_RECORD, null);
  expect(await commentsForDoc(getDb(env), URI)).toEqual([]);
});

it("ignores a comment on a doc this deployment doesn't know", async () => {
  const unknownSpace = `at:space/${OWNER}/network.habitat.docs.comments/zzz`;
  const unknownRecord = `${unknownSpace}/${BOB}/network.habitat.docs.comment/1`;
  await apply(unknownRecord, {
    $type: "network.habitat.docs.comment",
    body: "x",
    anchorStart: { $bytes: "YQ" },
    anchorEnd: { $bytes: "Yg" },
    createdAt: "2024-01-01T00:00:00.000Z",
  });
  expect(
    await commentsForDoc(
      getDb(env),
      `at:space/${OWNER}/network.habitat.docs/zzz`,
    ),
  ).toEqual([]);
});

it("ignores a comment record that fails lexicon validation", async () => {
  const valid = {
    $type: "network.habitat.docs.comment",
    body: "x",
    anchorStart: { $bytes: "YQ" },
    anchorEnd: { $bytes: "Yg" },
    createdAt: "2024-01-01T00:00:00.000Z",
  };
  for (const invalid of [
    { ...valid, anchorEnd: undefined }, // missing an anchor
    { ...valid, anchorStart: "YQ" }, // anchor as a string, not bytes
    { ...valid, $type: undefined }, // not typed as a comment
  ]) {
    await apply(COMMENT_RECORD, invalid);
  }
  expect(await commentsForDoc(getDb(env), URI)).toEqual([]);
});

const COMMENT_REPLY_RECORD = `${COMMENTS_SPACE}/${BOB}/network.habitat.docs.commentReply/xyz`;
const ROOT_COMMENT_URI = `${COMMENTS_SPACE}/${OWNER}/network.habitat.docs.comment/1`;
// A well-formed reply record. Its strongRef names a space record URI, which
// lexicon validation has to accept, and the strongRef's cid is format
// "cid", so this has to be a real one.
const REPLY_VALUE = {
  $type: "network.habitat.docs.commentReply",
  comment: {
    uri: ROOT_COMMENT_URI,
    cid: RECORD_CID,
  },
  body: "I agree",
  createdAt: "2024-01-01T00:00:00.000Z",
};

it("mirrors a commentReply record into comment_replies", async () => {
  await apply(COMMENT_REPLY_RECORD, REPLY_VALUE);
  const rows = await repliesForDoc(getDb(env), URI);
  expect(rows).toEqual([
    expect.objectContaining({
      uri: COMMENT_REPLY_RECORD,
      docSpaceUri: URI,
      commentUri: ROOT_COMMENT_URI,
      authorDid: BOB,
      body: "I agree",
      createdAt: Date.parse("2024-01-01T00:00:00.000Z"),
    }),
  ]);
});

it("removes the reply on a delete (null value)", async () => {
  await apply(COMMENT_REPLY_RECORD, REPLY_VALUE);
  expect(await repliesForDoc(getDb(env), URI)).toHaveLength(1);
  await apply(COMMENT_REPLY_RECORD, null);
  expect(await repliesForDoc(getDb(env), URI)).toEqual([]);
});

it("ignores a commentReply record that fails lexicon validation", async () => {
  for (const invalid of [
    { ...REPLY_VALUE, comment: undefined }, // missing its comment ref
    { ...REPLY_VALUE, body: undefined }, // missing its body
    { ...REPLY_VALUE, comment: { uri: "not-a-uri", cid: "x" } }, // bad ref
    { ...REPLY_VALUE, $type: undefined }, // not typed as a reply
  ]) {
    await apply(COMMENT_REPLY_RECORD, invalid);
  }
  expect(await repliesForDoc(getDb(env), URI)).toEqual([]);
});

// Whole events, as the stream hands them to the app's indexer.
describe("doc indexing", () => {
  const BOB_DID = "did:web:bob.example" as DidString;
  const SPACE = URI as SpaceRefString;
  const COLLECTION = "network.habitat.relationship.userRelation";
  const grant = (subject: string) =>
    jsonToLex({ subject, relation: "reader" }) as LexMap;
  const carol = "did:web:carol.example";

  it("applies writes and deletes", async () => {
    await indexEvents(putEvent(SPACE, BOB_DID, COLLECTION, "r1", grant(carol)));
    expect(await docsFor(getDb(env), carol)).toHaveLength(1);
    await indexEvents(delEvent(SPACE, BOB_DID, COLLECTION, "r1"));
    expect(await docsFor(getDb(env), carol)).toEqual([]);
  });

  it("a deleted space clears the mirrored records", async () => {
    await indexEvents(
      putEvent(SPACE, BOB_DID, COLLECTION, "r1", grant(carol)),
      spaceDeletedEvent(SPACE),
    );
    expect(await docsFor(getDb(env), carol)).toEqual([]);
  });
});
