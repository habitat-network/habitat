import { env, runInDurableObject } from "cloudflare:test";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { fileByUri, getDb, upsertConnectedOrg } from "@/db";
import { connectedOrgs, files } from "@/db/schema";
import { putFileRecord, deleteFileRecord } from "@/server/files/files.server";
import { followOrg } from "@/server/stream";
import type { OrgStream } from "@/server/stream/orgStream";
import { TEST_DID_HEADER } from "./fakeOAuth";

vi.mock("../src/server/oauth", () => import("./fakeOAuth"));

// The group's stream end to end inside workerd: a request against the org
// (followOrg, on the org's Durable Object) catches D1 up over getSnapshot, then opens pear's subscribe
// stream through the group's session (a WebSocket upgrade through fetch, as
// in production) and indexes what it carries into D1. A write made through
// it returns only once its event has been indexed. The org's Durable Object
// has no alarm: concurrent requests share one catch-up and one socket, and
// nothing runs until a request asks.

const ORG = "did:web:org.example";
const SPACE = `at://${ORG}/space/network.habitat.drop/3kfile`;
const URI = `${SPACE}/${ORG}/network.habitat.drop.file/self`;
const BLOB = {
  $type: "blob",
  ref: { $link: "bafkreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku" },
  mimeType: "text/plain",
  size: 11,
};
const CID1 = "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm";
const CID2 = "bafyreihbb7oqfrxctnelmhaswbqmdbkj2zhx6bmbrbnyuo3s5vetbmwamu";

function fileCommit(seq: number, cid: string, name: string) {
  return JSON.stringify({
    $type: "message",
    payload: {
      $type: "network.habitat.space.subscribe#commit",
      seq,
      space: SPACE,
      did: ORG,
      uri: URI,
      time: "2026-11-20T00:00:00.000000Z",
      rev: "3l3qo2vutsw2b",
      operation: seq === 1 ? "create" : "update",
      collection: "network.habitat.drop.file",
      rkey: "self",
      cid,
      record: {
        $type: "network.habitat.drop.file",
        name,
        file: BLOB,
        uploadedBy: "did:plc:alice",
        createdAt: "2026-10-08T12:00:00.000Z",
      },
    },
  });
}

function fileDelete(seq: number) {
  return JSON.stringify({
    $type: "message",
    payload: {
      $type: "network.habitat.space.subscribe#commit",
      seq,
      space: SPACE,
      did: ORG,
      uri: URI,
      time: "2026-11-20T00:00:00.000000Z",
      rev: "3l3qo2vutsw2c",
      operation: "delete",
      collection: "network.habitat.drop.file",
      rkey: "self",
    },
  });
}

// pear stands in for the space host: the subscribe upgrade gets the server
// half of a WebSocketPair, and writes are answered (and their events sent
// down the stream) as pear would.
let socket: WebSocket | undefined;
let subscribeUrls: URL[] = [];
let snapshotUrls: URL[] = [];
let recordExists = false;
const writes: { nsid: string; did: string | null; body: unknown }[] = [];

beforeEach(async () => {
  socket = undefined;
  subscribeUrls = [];
  snapshotUrls = [];
  recordExists = false;
  writes.length = 0;
  await getDb(env).delete(files);
  await getDb(env).delete(connectedOrgs);
  await upsertConnectedOrg(getDb(env), {
    orgDid: ORG,
    name: "Org",
    connectedBy: "did:plc:alice",
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const req = new Request(input, init);
      const url = new URL(req.url);
      const nsid = url.pathname.replace("/xrpc/", "");
      if (nsid === "network.habitat.space.getSnapshot") {
        expect(req.headers.get(TEST_DID_HEADER)).toBe(ORG);
        snapshotUrls.push(url);
        // As pear does: afterSeq must be a non-negative integer.
        if (!(Number(url.searchParams.get("afterSeq")) >= 0)) {
          return Response.json({ error: "InvalidRequest" }, { status: 400 });
        }
        // One event at seq 1, which is also the tip.
        return Response.json({
          events: [JSON.parse(fileCommit(1, CID1, "notes.txt")).payload],
          plannedThroughSeq: 1,
          tipSeq: 1,
        });
      }
      if (nsid === "network.habitat.space.subscribe") {
        expect(req.headers.get("Upgrade")).toBe("websocket");
        expect(req.headers.get(TEST_DID_HEADER)).toBe(ORG);
        subscribeUrls.push(url);
        const pair = new WebSocketPair();
        pair[1].accept();
        socket = pair[1];
        return new Response(null, { status: 101, webSocket: pair[0] });
      }
      const did = req.headers.get(TEST_DID_HEADER);
      if (nsid === "com.atproto.space.getRecord") {
        return recordExists
          ? Response.json({ uri: URI, cid: CID2, value: {} })
          : Response.json({ error: "RecordNotFound" }, { status: 400 });
      }
      const body = await req.json();
      writes.push({ nsid, did, body });
      if (nsid === "com.atproto.space.putRecord") {
        // The event follows the response, as it would from pear's sequencer.
        setTimeout(() => socket?.send(fileCommit(2, CID2, "renamed.txt")), 20);
        return Response.json({ uri: URI, cid: CID2 });
      }
      if (nsid === "com.atproto.space.deleteRecord") {
        setTimeout(() => socket?.send(fileDelete(3)), 20);
        return Response.json({});
      }
      return Response.json({ error: "NotFound" }, { status: 404 });
    }),
  );
});

const stub = () => env.ORG_STREAM.get(env.ORG_STREAM.idFromName(ORG));

afterEach(() => stub().stopStream());

async function until(cond: () => Promise<boolean>) {
  for (let i = 0; i < 200; i++) {
    if (await cond()) return;
    await new Promise((r) => setTimeout(r, 10));
  }
  expect(await cond()).toBe(true);
}

it("catches up on a request, follows live, and writes return once indexed", async () => {
  const db = getDb(env);
  await followOrg(env, ORG);

  // Caught up over getSnapshot from the start (afterSeq is exclusive and
  // pear refuses a negative one, so a fresh stream asks after 0),
  // asking only for what the app indexes, before the socket opened.
  expect(snapshotUrls).toHaveLength(1);
  expect(snapshotUrls[0].searchParams.get("afterSeq")).toBe("0");
  expect(snapshotUrls[0].searchParams.getAll("collections")).toContain(
    "network.habitat.drop.file",
  );
  expect((await fileByUri(db, URI))?.name).toBe("notes.txt");

  // Then live, from the seq after the snapshot's tip.
  await until(async () => socket !== undefined);
  expect(subscribeUrls[0].searchParams.get("cursor")).toBe("2");

  // A write made through the stream is in D1 by the time it returns.
  const written = await putFileRecord(env, ORG, SPACE, {
    name: "renamed.txt",
    blob: BLOB,
    uploadedBy: "did:plc:alice",
  });
  expect(written).toEqual({ uri: URI, cid: CID2, indexed: true });
  expect((await fileByUri(db, URI))?.name).toBe("renamed.txt");
  expect(writes[0]).toMatchObject({
    nsid: "com.atproto.space.putRecord",
    did: ORG,
    body: { space: SPACE, repo: ORG, rkey: "self" },
  });

  recordExists = true;
  await deleteFileRecord(env, ORG, SPACE);
  expect(await fileByUri(db, URI)).toBeUndefined();

  // The cursor is kept past the delete's seq, and nothing is scheduled to
  // wake the object.
  await runInDurableObject(stub(), async (_: OrgStream, state) => {
    expect(await state.storage.get("cursor")).toBe(4);
    expect(await state.storage.getAlarm()).toBeNull();
  });
  // Nothing re-opened the stream along the way.
  expect(snapshotUrls).toHaveLength(1);
  expect(subscribeUrls).toHaveLength(1);
  // The first run loads the sinks and the SDK, which takes a few seconds on
  // CI runners.
}, 30_000);

it("shares one catch-up and one socket between concurrent requests", async () => {
  // Through separate stubs, as separate requests (isolates) would.
  await Promise.all(Array.from({ length: 5 }, () => followOrg(env, ORG)));
  await followOrg(env, ORG);
  await until(async () => socket !== undefined);
  expect(snapshotUrls).toHaveLength(1);
  expect(subscribeUrls).toHaveLength(1);
}, 30_000);

it("starts again from the saved cursor once the stream has stopped", async () => {
  await followOrg(env, ORG);
  await until(async () => socket !== undefined);
  expect(await stub().cursor()).toBe(2);
  // What an evicted isolate looks like to the next request.
  await stub().stopStream();
  expect(await stub().isRunning()).toBe(false);
  await until(async () => {
    await followOrg(env, ORG);
    return subscribeUrls.length === 2;
  });
  // Resumed after the saved cursor: the snapshot asks only for what's newer.
  expect(snapshotUrls[1].searchParams.get("afterSeq")).toBe("1");
}, 30_000);

it("does nothing for an org that isn't connected", async () => {
  await followOrg(env, "did:web:other.example");
  const other = env.ORG_STREAM.get(
    env.ORG_STREAM.idFromName("did:web:other.example"),
  );
  expect(await other.isRunning()).toBe(false);
  expect(snapshotUrls).toHaveLength(0);
  expect(subscribeUrls).toHaveLength(0);
});
