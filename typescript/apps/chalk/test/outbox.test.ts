import { env } from "cloudflare:test";
import { beforeEach, expect, it, vi } from "vitest";
import * as Y from "yjs";
import { processOutboxMessage } from "../src/server/outbox";
import { getDb, upsertDoc, docsFor } from "../src/db";

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

it("ignores a malformed uri", async () => {
  await processOutboxMessage(env, msg("not-a-uri", "cid1"));
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
