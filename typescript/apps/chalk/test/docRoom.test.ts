import { env, runInDurableObject } from "cloudflare:test";
import { expect, it } from "vitest";
import * as Y from "yjs";
import type { DocRoom } from "../src/server/rooms/docRoom";

const URI = "at://did:web:alice.example/space/network.habitat.docs/abc";
const ID = { spaceUri: URI, ownerDid: "did:web:alice.example" };

function updateFrom(fn: (d: Y.Doc) => void): Uint8Array {
  const d = new Y.Doc();
  fn(d);
  return Y.encodeStateAsUpdateV2(d);
}

function textOf(bytes: Uint8Array): string {
  const d = new Y.Doc();
  Y.applyUpdateV2(d, bytes);
  return d.getText("body").toString();
}

it("merges an update and exposes it in the snapshot", async () => {
  const stub = env.DOC.get(env.DOC.idFromName(URI));
  const update = updateFrom((d) => d.getText("body").insert(0, "hello"));
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(ID, "did:web:bob.example", update),
  );
  const snap = await runInDurableObject(stub, (r: DocRoom) => r.snapshot());
  expect(textOf(snap)).toBe("hello");
});

it("merges concurrent updates from two members", async () => {
  const stub = env.DOC.get(env.DOC.idFromName(URI + "-2"));
  const id = { spaceUri: URI + "-2", ownerDid: ID.ownerDid };
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(
      id,
      "did:web:bob.example",
      updateFrom((d) => d.getText("body").insert(0, "a")),
    ),
  );
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(
      id,
      "did:web:carol.example",
      updateFrom((d) => d.getText("body").insert(0, "b")),
    ),
  );
  expect(
    textOf(await runInDurableObject(stub, (r: DocRoom) => r.snapshot())),
  ).toHaveLength(2);
});

it("re-applying the same update is a no-op", async () => {
  const stub = env.DOC.get(env.DOC.idFromName(URI + "-3"));
  const id = { spaceUri: URI + "-3", ownerDid: ID.ownerDid };
  const update = updateFrom((d) => d.getText("body").insert(0, "xy"));
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(id, "did:web:bob.example", update),
  );
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(id, "did:web:bob.example", update),
  );
  expect(
    textOf(await runInDurableObject(stub, (r: DocRoom) => r.snapshot())),
  ).toBe("xy");
});

it("remembers its identity so alarm-driven work can use it", async () => {
  const stub = env.DOC.get(env.DOC.idFromName(URI + "-4"));
  const id = { spaceUri: URI + "-4", ownerDid: "did:web:dave.example" };
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(
      id,
      "did:web:bob.example",
      updateFrom((d) => d.getText("body").insert(0, "z")),
    ),
  );
  await runInDurableObject(stub, async (r: DocRoom) => {
    expect(await r.identity()).toEqual(id);
  });
});

// createDoc calls seedIdentity on a brand-new room, before anyone has typed
// anything. Regression test: this was originally an `applyEdit` with
// `new Uint8Array(0)` as a supposedly-empty update, which made every
// createDoc fail with "Unexpected end of array" — there is no zero-length
// Yjs update.
it("seeds identity on an untouched room without scheduling a flush", async () => {
  const uri = URI + "-6";
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  const id = { spaceUri: uri, ownerDid: "did:web:erin.example" };
  await runInDurableObject(stub, (r: DocRoom) => r.seedIdentity(id));
  await runInDurableObject(stub, async (r: DocRoom, state) => {
    expect(await r.identity()).toEqual(id);
    expect(await r.snapshot()).toEqual(Y.encodeStateAsUpdateV2(new Y.Doc()));
    expect(await state.storage.getAlarm()).toBeNull();
  });
});

it("keeps a known ownerDid when a later caller omits it", async () => {
  const stub = env.DOC.get(env.DOC.idFromName(URI + "-5"));
  const uri = URI + "-5";
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(
      { spaceUri: uri, ownerDid: "did:web:dave.example" },
      "did:web:bob.example",
      updateFrom((d) => d.getText("body").insert(0, "1")),
    ),
  );
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(
      { spaceUri: uri },
      "did:web:bob.example",
      updateFrom((d) => d.getText("body").insert(0, "2")),
    ),
  );
  await runInDurableObject(stub, async (r: DocRoom) => {
    expect((await r.identity()).ownerDid).toBe("did:web:dave.example");
  });
});
