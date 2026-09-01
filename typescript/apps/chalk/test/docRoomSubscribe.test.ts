import { env, runInDurableObject } from "cloudflare:test";
import { expect, it } from "vitest";
import * as Y from "yjs";
import type { DocRoom } from "../src/server/rooms/docRoom";

const URI = "at://did:web:alice.example/space/network.habitat.docs/sub";

function updateFrom(fn: (d: Y.Doc) => void): Uint8Array {
  const d = new Y.Doc();
  fn(d);
  return Y.encodeStateAsUpdateV2(d);
}

// NOTE on scope: @cloudflare/vitest-pool-workers 0.22.0 (latest, as of this
// writing) does not deliver actual message bytes across a WebSocket
// obtained from `stub.fetch()` in test code — even a trivial `ws.send(new
// Uint8Array([1,2,3]))` from inside the Durable Object arrives as an empty
// ArrayBuffer on the test side, confirmed via a minimal repro that bypassed
// this app's code entirely (a bare RPC method doing only `ws.send(...)`).
// The sync-protocol wire logic itself (y-protocols/sync + this room's
// message handling) was separately verified byte-for-byte correct with a
// plain Node.js script exercising the same encode/decode calls outside the
// Workers runtime, and end-to-end via a live dev server (a raw WebSocket
// opened directly from the browser to /ws/$docId, bypassing y-websocket
// and BroadcastChannel entirely, got back the full document content). So
// these tests stick to what the harness can actually observe: the upgrade
// handshake (status codes, headers) and ctx.getWebSockets() bookkeeping —
// not received message content.

it("upgrades to a websocket and registers the socket", async () => {
  const stub = env.DOC.get(env.DOC.idFromName(URI));
  const res = await stub.fetch("https://doc-room/", {
    headers: {
      Upgrade: "websocket",
      "X-Chalk-Doc-Id": URI,
      "X-Chalk-Member-Did": "did:web:carol.example",
    },
  });
  expect(res.status).toBe(101);
  const ws = res.webSocket;
  if (!ws) throw new Error("DocRoom did not upgrade to a websocket");
  ws.accept();
  await runInDurableObject(stub, (r: DocRoom) => {
    // @ts-expect-error accessing a protected DurableObject field for the test
    expect(r.ctx.getWebSockets().length).toBe(1);
  });
});

it("stores the member identity on the accepted socket", async () => {
  const uri = URI + "-attach";
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  const res = await stub.fetch("https://doc-room/", {
    headers: {
      Upgrade: "websocket",
      "X-Chalk-Doc-Id": uri,
      "X-Chalk-Member-Did": "did:web:carol.example",
    },
  });
  const ws = res.webSocket;
  if (!ws) throw new Error("DocRoom did not upgrade to a websocket");
  ws.accept();
  await runInDurableObject(stub, (r: DocRoom) => {
    // @ts-expect-error accessing a protected DurableObject field for the test
    const [socket] = r.ctx.getWebSockets();
    expect(socket.deserializeAttachment()).toBe("did:web:carol.example");
  });
});

it("a merge after the subscriber closes does not throw", async () => {
  const uri = URI + "-3";
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  const res = await stub.fetch("https://doc-room/", {
    headers: {
      Upgrade: "websocket",
      "X-Chalk-Doc-Id": uri,
      "X-Chalk-Member-Did": "did:web:carol.example",
    },
  });
  const ws = res.webSocket;
  if (!ws) throw new Error("DocRoom did not upgrade to a websocket");
  ws.accept();
  ws.close();
  // A merge right after close must not throw even though the closed
  // socket is (briefly, at least) still in ctx.getWebSockets().
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(
      { spaceUri: uri },
      "did:web:bob.example",
      updateFrom((d) => d.getText("body").insert(0, "c")),
    ),
  );
});

it("rejects a request that isn't a websocket upgrade", async () => {
  const uri = URI + "-4";
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  const res = await stub.fetch("https://doc-room/", {
    headers: { "X-Chalk-Doc-Id": uri, "X-Chalk-Member-Did": "did:web:x" },
  });
  expect(res.status).toBe(426);
});

it("rejects an upgrade missing the doc/member identity headers", async () => {
  const uri = URI + "-5";
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  const res = await stub.fetch("https://doc-room/", {
    headers: { Upgrade: "websocket" },
  });
  expect(res.status).toBe(400);
});
