import { env, runInDurableObject } from "cloudflare:test";
import { expect, it } from "vitest";
import * as Y from "yjs";
import type { DocRoom } from "../src/server/rooms/docRoom";
import { readFrames } from "../src/server/rooms/frames";

const URI = "at://did:web:alice.example/space/network.habitat.docs/sub";
const ID = { spaceUri: URI, ownerDid: "did:web:alice.example" };

function updateFrom(fn: (d: Y.Doc) => void): Uint8Array {
  const d = new Y.Doc();
  fn(d);
  return Y.encodeStateAsUpdateV2(d);
}

it("sends the current snapshot as the first frame", async () => {
  const stub = env.DOC.get(env.DOC.idFromName(URI));
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(
      ID,
      "did:web:bob.example",
      updateFrom((d) => d.getText("body").insert(0, "hi")),
    ),
  );
  const stream = await stub.subscribe();
  const frames = readFrames(stream);
  const first = (await frames.next()).value as Uint8Array;
  const d = new Y.Doc();
  Y.applyUpdateV2(d, first);
  expect(d.getText("body").toString()).toBe("hi");
});

it("pushes subsequent merges to a live subscriber", async () => {
  const uri = URI + "-2";
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  const id = { spaceUri: uri, ownerDid: ID.ownerDid };
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(
      id,
      "did:web:bob.example",
      updateFrom((d) => d.getText("body").insert(0, "a")),
    ),
  );
  const stream = await stub.subscribe();
  const frames = readFrames(stream);
  await frames.next(); // snapshot
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(
      id,
      "did:web:carol.example",
      updateFrom((d) => d.getText("body").insert(0, "b")),
    ),
  );
  const next = (await frames.next()).value as Uint8Array;
  const d = new Y.Doc();
  Y.applyUpdateV2(d, next);
  expect(d.getText("body").toString()).toHaveLength(2);
});

it("drops a subscriber whose stream is cancelled without failing merges", async () => {
  const uri = URI + "-3";
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  const stream = await stub.subscribe();
  const reader = stream.getReader();
  await reader.read(); // pump the snapshot frame through before cancelling
  await reader.cancel();
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(
      { spaceUri: uri },
      "did:web:bob.example",
      updateFrom((d) => d.getText("body").insert(0, "c")),
    ),
  );
  await runInDurableObject(stub, async (r: DocRoom) => {
    expect(await r.subscriberCount()).toBe(0);
  });
});
