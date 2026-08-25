import { env, runInDurableObject } from "cloudflare:test";
import { beforeEach, expect, it, vi } from "vitest";
import * as Y from "yjs";
import type { SapChannel } from "../src/server/rooms/sapChannel";
import { getDb, upsertDoc } from "../src/db";

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
  const stub = env.SAP.get(env.SAP.idFromName("default"));
  // A fresh Response per call, not a shared instance: this Response's body
  // ends up read inside DocRoom (via applyRemote -> getBlob), a different
  // Durable Object than the one running this test's setup code — reusing an
  // instance created here hits a real Workers I/O-ownership restriction
  // ("Cannot perform I/O on behalf of a different Durable Object").
  fetchMock.mockImplementation(async () => new Response(EMPTY_UPDATE));
  await runInDurableObject(stub, (c: SapChannel) =>
    c.handleOutboxMessage(msg(RECORD, "cid1")),
  );
  expect(
    fetchMock.mock.calls.some((c) => String(c[0]).includes("space.getBlob")),
  ).toBe(true);
});

it("ignores a record in a different collection", async () => {
  const stub = env.SAP.get(env.SAP.idFromName("default"));
  const other = `${URI}/did:web:bob.example/network.habitat.docs.markdown/self`;
  await runInDurableObject(stub, (c: SapChannel) =>
    c.handleOutboxMessage(msg(other, "cid1")),
  );
  expect(fetchMock).not.toHaveBeenCalled();
});

it("ignores a doc absent from the index", async () => {
  const stub = env.SAP.get(env.SAP.idFromName("default"));
  const unknown = `at://${OWNER}/space/network.habitat.docs/zzz/did:web:bob.example/network.habitat.docs.crdt/self`;
  await runInDurableObject(stub, (c: SapChannel) =>
    c.handleOutboxMessage(msg(unknown, "cid1")),
  );
  expect(fetchMock).not.toHaveBeenCalled();
});

it("ignores a record with no blob reference", async () => {
  const stub = env.SAP.get(env.SAP.idFromName("default"));
  await runInDurableObject(stub, (c: SapChannel) =>
    c.handleOutboxMessage(msg(RECORD, undefined)),
  );
  expect(fetchMock).not.toHaveBeenCalled();
});

it("ignores a malformed uri", async () => {
  const stub = env.SAP.get(env.SAP.idFromName("default"));
  await runInDurableObject(stub, (c: SapChannel) =>
    c.handleOutboxMessage(msg("not-a-uri", "cid1")),
  );
  expect(fetchMock).not.toHaveBeenCalled();
});
