import {
  env,
  runInDurableObject,
  runDurableObjectAlarm,
} from "cloudflare:test";
import { beforeEach, expect, it, vi } from "vitest";
import * as Y from "yjs";
import type { DocRoom } from "../src/server/rooms/docRoom";

const URI = "at://did:web:alice.example/space/network.habitat.docs/flush";
const ID = { spaceUri: URI, ownerDid: "did:web:alice.example" };

const fetchMock = vi.fn();
beforeEach(() => {
  fetchMock.mockReset();
  // A fresh Response per call, not a shared instance: flushMember makes two
  // fetch calls (uploadBlob then putRecord), each reading its own body via
  // .json() — a single Response instance can only have its body read once.
  fetchMock.mockImplementation(
    async () =>
      new Response(
        JSON.stringify({ blob: { ref: { $link: "cid1" } }, cid: "cid1" }),
      ),
  );
  vi.stubGlobal("fetch", fetchMock);
});

function updateFrom(fn: (d: Y.Doc) => void): Uint8Array {
  const d = new Y.Doc();
  fn(d);
  return Y.encodeStateAsUpdateV2(d);
}

it("does not write to sap before the debounce fires", async () => {
  const stub = env.DOC.get(env.DOC.idFromName(URI));
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(
      ID,
      "did:web:bob.example",
      updateFrom((d) => d.getText("body").insert(0, "x")),
    ),
  );
  expect(fetchMock).not.toHaveBeenCalled();
});

it("schedules an alarm on the first edit", async () => {
  const uri = URI + "-2";
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(
      { spaceUri: uri },
      "did:web:bob.example",
      updateFrom((d) => d.getText("body").insert(0, "x")),
    ),
  );
  await runInDurableObject(stub, async (r: DocRoom, state) => {
    expect(await state.storage.getAlarm()).not.toBeNull();
  });
});

it("uploads a blob and putRecords to the member's own repo when the alarm fires", async () => {
  const uri = URI + "-3";
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(
      { spaceUri: uri },
      "did:web:bob.example",
      updateFrom((d) => d.getText("body").insert(0, "x")),
    ),
  );
  // runDurableObjectAlarm runs the alarm handler unconditionally — it does
  // not fast-forward this environment's Date.now() to the alarm's scheduled
  // time — so alarm()'s own idleDeadline/firstPushAt due-check (a real
  // production guard: a DO's single alarm can have several pending rows
  // with different deadlines, and only the ones actually due should flush)
  // would otherwise skip a row scheduled moments ago. Wait past IDLE_MS for
  // real, matching what a live alarm firing at its scheduled time sees.
  await new Promise((resolve) => setTimeout(resolve, 2100));
  await runDurableObjectAlarm(stub);
  const urls = fetchMock.mock.calls.map((c) => String(c[0]));
  expect(urls.some((u) => u.includes("network.habitat.repo.uploadBlob"))).toBe(
    true,
  );
  // Earlier tests' real alarms (also scheduled IDLE_MS out) can fire in the
  // background during this test's real wait above, so filter on this test's
  // own space URI rather than assuming the first/only "space.putRecord" call
  // observed belongs to it.
  const put = fetchMock.mock.calls.find(
    (c) =>
      String(c[0]).includes("space.putRecord") &&
      JSON.parse(String(c[1].body)).space === uri,
  );
  expect(JSON.parse(String(put![1].body))).toMatchObject({
    space: uri,
    repo: "did:web:bob.example",
    collection: "network.habitat.docs.crdt",
    rkey: "self",
  });
});

it("flushes each member to their own repo separately", async () => {
  const uri = URI + "-4";
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  for (const did of ["did:web:bob.example", "did:web:carol.example"]) {
    await runInDurableObject(stub, (r: DocRoom) =>
      r.applyEdit(
        { spaceUri: uri },
        did,
        updateFrom((d) => d.getText("body").insert(0, did[8]!)),
      ),
    );
  }
  await new Promise((resolve) => setTimeout(resolve, 2100)); // see the note above
  await runDurableObjectAlarm(stub);
  const repos = fetchMock.mock.calls
    .filter((c) => String(c[0]).includes("space.putRecord"))
    .map(
      (c) => JSON.parse(String(c[1].body)) as { space: string; repo: string },
    )
    .filter((body) => body.space === uri)
    .map((body) => body.repo);
  expect(new Set(repos)).toEqual(
    new Set(["did:web:bob.example", "did:web:carol.example"]),
  );
});

it("keeps the pending flush when the sap write fails", async () => {
  const uri = URI + "-retry";
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(
      { spaceUri: uri },
      "did:web:bob.example",
      updateFrom((d) => d.getText("body").insert(0, "x")),
    ),
  );
  // sap is down for this alarm. The entry must survive so the alarm retry
  // Cloudflare schedules after a thrown handler actually has something left
  // to flush — otherwise this edit is never written to the member's repo.
  fetchMock.mockImplementation(async () => {
    throw new Error("sap is down");
  });
  await new Promise((resolve) => setTimeout(resolve, 2100)); // see the note above
  await runDurableObjectAlarm(stub).catch(() => {});
  await runInDurableObject(stub, async (_r: DocRoom, state) => {
    const pending = await state.storage.list({ prefix: "pending:" });
    expect([...pending.keys()]).toContain("pending:member:did:web:bob.example");
  });
});
