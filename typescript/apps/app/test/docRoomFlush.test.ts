import { env, runInDurableObject } from "cloudflare:test";
import { warmHabitat } from "./warmHabitat";
import { beforeAll, beforeEach, expect, it, vi } from "vitest";
import * as Y from "yjs";
import type { DocRoom } from "../src/server/docs/rooms/docRoom";
import { cancelAlarm, runAlarmNow } from "./docRoomAlarm";

vi.mock("../src/server/oauth", () => import("./fakeOAuth"));

const URI = "at://did:web:alice.example/space/network.habitat.docs/flush";
const ID = { spaceUri: URI, ownerDid: "did:web:alice.example" };

beforeAll(warmHabitat, 60_000);

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

it("does not write to pear before the debounce fires", async () => {
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
  // runAlarmNow (test/docRoomAlarm.ts) makes the flush due and runs the alarm
  // to completion, with no real waiting and no background alarm.
  await runAlarmNow(stub);
  const urls = fetchMock.mock.calls.map((c) => String(c[0]));
  expect(urls.some((u) => u.includes("network.habitat.repo.uploadBlob"))).toBe(
    true,
  );
  // Filter on this test's own space URI: the mock is shared across tests.
  const put = fetchMock.mock.calls.find(
    (c) =>
      String(c[0]).includes("space.putRecord") &&
      JSON.parse(String(c[1].body)).space === uri,
  );
  expect(put).toBeDefined();
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
  await runAlarmNow(stub);
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

it("keeps the pending flush when the pear write fails", async () => {
  const uri = URI + "-retry";
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(
      { spaceUri: uri },
      "did:web:bob.example",
      updateFrom((d) => d.getText("body").insert(0, "x")),
    ),
  );
  // pear is down for this alarm. The entry must survive so the alarm retry
  // Cloudflare schedules after a thrown handler actually has something left
  // to flush — otherwise this edit is never written to the member's repo.
  fetchMock.mockImplementation(async () => {
    throw new Error("pear is down");
  });
  await runAlarmNow(stub).catch(() => {});
  await runInDurableObject(stub, async (_r: DocRoom, state) => {
    const pending = await state.storage.list({ prefix: "pending:" });
    expect([...pending.keys()]).toContain("pending:member:did:web:bob.example");
  });
  // Don't leave a failing alarm to fire (and throw) after the test is over.
  await cancelAlarm(stub);
});
