import { env } from "cloudflare:test";
import { beforeEach, expect, it, vi } from "vitest";
import * as Y from "yjs";
import { processOutboxMessage } from "../src/server/outbox";
import { getDb, upsertDoc, docsFor, docByUri } from "../src/db";

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

const SPACE_HOST = "https://space-host.test";

// fakeSap answers the calls applyRemote and indexUnknownDoc make: sap's
// /space/credential with a credential for SPACE_HOST, listRelations via the
// given callback, and anything else (getBlob) with an empty Yjs update. A
// fresh Response per call, not a shared instance: a Response's body ends up
// read inside DocRoom (via applyRemote -> getSpaceBlob), a different Durable
// Object than this test's own execution context — reusing an instance
// created here hits a real Workers I/O-ownership restriction ("Cannot perform
// I/O on behalf of a different Durable Object").
function fakeSap(
  listRelations: () => Response = () => Response.json({ relations: [] }),
) {
  return async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes("/space/credential")) {
      return Response.json({ credential: "space-cred", host: SPACE_HOST });
    }
    if (url.includes("relationship.listRelations")) return listRelations();
    return new Response(EMPTY_UPDATE);
  };
}

function msg(uri: string, cid: string | undefined) {
  return { id: 1, uri, value: cid ? { blob: { ref: { $link: cid } } } : {} };
}

it("routes a crdt record to its doc room, reading the blob with a space credential", async () => {
  fetchMock.mockImplementation(fakeSap());
  await processOutboxMessage(env, msg(RECORD, "cid1"));
  const blobCall = fetchMock.mock.calls.find((c) =>
    String(c[0]).startsWith(`${SPACE_HOST}/xrpc/network.habitat.space.getBlob`),
  );
  expect(blobCall).toBeDefined();
  expect(
    new Headers((blobCall?.[1] as RequestInit | undefined)?.headers).get(
      "Authorization",
    ),
  ).toBe("Bearer space-cred");
});

it("ignores a record in a different collection", async () => {
  const other = `${URI}/did:web:bob.example/network.habitat.docs.comment/c1`;
  await processOutboxMessage(env, msg(other, "cid1"));
  expect(fetchMock).not.toHaveBeenCalled();
});

const UNKNOWN_URI = `at://${OWNER}/space/network.habitat.docs/zzz`;
const UNKNOWN_RECORD = `${UNKNOWN_URI}/did:web:bob.example/network.habitat.docs.crdt/self`;
const CAROL = "did:web:carol.example";

it("indexes a doc absent from the index under its owner relation", async () => {
  fetchMock.mockImplementation(
    fakeSap(() =>
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
  await processOutboxMessage(env, msg(UNKNOWN_RECORD, "cid1"));

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
  expect(
    fetchMock.mock.calls.some((c) => String(c[0]).includes("space.getBlob")),
  ).toBe(true);
});

it("falls back to the space authority when a doc has no owner relation", async () => {
  fetchMock.mockImplementation(fakeSap(() => Response.json({ relations: [] })));
  await processOutboxMessage(env, msg(UNKNOWN_RECORD, "cid1"));
  expect((await docByUri(getDb(env), UNKNOWN_URI))?.ownerDid).toBe(OWNER);
  expect(
    fetchMock.mock.calls.some((c) => String(c[0]).includes("space.getBlob")),
  ).toBe(true);
});

it("falls back to the space authority when the owner lookup fails", async () => {
  fetchMock.mockImplementation(
    fakeSap(() => new Response("no tracked session", { status: 502 })),
  );
  await processOutboxMessage(env, msg(UNKNOWN_RECORD, "cid1"));
  expect((await docByUri(getDb(env), UNKNOWN_URI))?.ownerDid).toBe(OWNER);
});

const MARKDOWN_RECORD = `${URI}/${OWNER}/network.habitat.docs.markdown/self`;

it("sets a doc's title from its markdown record", async () => {
  await processOutboxMessage(env, {
    id: 1,
    uri: MARKDOWN_RECORD,
    value: { title: "Quarterly plan", content: "Quarterly plan\n\nDetails" },
  });
  expect((await docByUri(getDb(env), URI))?.title).toBe("Quarterly plan");
});

it("indexes a doc absent from the index under its markdown record's title", async () => {
  fetchMock.mockImplementation(
    fakeSap(() =>
      Response.json({
        relations: [
          { uri: "r1", subject: CAROL, relation: "owner", object: UNKNOWN_URI },
        ],
      }),
    ),
  );
  await processOutboxMessage(env, {
    id: 1,
    uri: `${UNKNOWN_URI}/${CAROL}/network.habitat.docs.markdown/self`,
    value: { title: "Launch notes", content: "Launch notes" },
  });
  expect(await docByUri(getDb(env), UNKNOWN_URI)).toEqual({
    docId: UNKNOWN_URI,
    uri: UNKNOWN_URI,
    ownerDid: CAROL,
    title: "Launch notes",
  });
});

it("ignores a markdown delete tombstone", async () => {
  await processOutboxMessage(env, { id: 1, uri: MARKDOWN_RECORD, value: null });
  expect((await docByUri(getDb(env), URI))?.title).toBe("Untitled");
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
