// @vitest-environment node
import type { VerifiedRecord } from "@atproto/space";
import { describe, expect, it } from "vitest";
import { server } from "../test/msw";
import {
  type PromiseRepoBatch,
  type PromiseSyncStore,
  createSpaceSyncer,
} from "./promise";
import { FakeNetwork, PLC_URL } from "./test/fakeNetwork";
import { SERVICE_DID } from "./test/harness";
import type { RepoState, SpaceState } from "./types";

const memoryStore = (): PromiseSyncStore => {
  const spaces = new Map<string, SpaceState>();
  const repos = new Map<string, RepoState>();
  return {
    getSpace: async (s) => spaces.get(s),
    putSpace: async (st) => void spaces.set(st.space, st),
    removeSpace: async (s) => {
      spaces.delete(s);
      for (const k of [...repos.keys()])
        if (k.startsWith(`${s}|`)) repos.delete(k);
    },
    dueSpaces: async (now, limit) =>
      [...spaces.values()].filter((s) => s.nextDueAt <= now).slice(0, limit),
    getRepo: async (s, d) => repos.get(`${s}|${d}`),
    listRepoDids: async (s) =>
      [...repos.values()].filter((r) => r.space === s).map((r) => r.did),
    putRepo: async (r) => void repos.set(`${r.space}|${r.did}`, r),
    removeRepo: async (s, d) => void repos.delete(`${s}|${d}`),
  };
};

describe("createSpaceSyncer", () => {
  it("syncs through Promise ports and verifies notifications", async () => {
    const net = new FakeNetwork();
    server.use(...net.handlers);
    const alice = await net.createAccount("alice");
    const space = net.createSpace(alice);
    await space.write(alice, "com.example.post", "1", { text: "a" });

    const applied: Array<{ tag: string; records?: string[] }> = [];
    const syncer = createSpaceSyncer({
      serviceDid: SERVICE_DID,
      plcUrl: PLC_URL,
      store: memoryStore(),
      delegation: { issue: (s) => net.delegationTokenFor(s) },
      sink: {
        apply: async (batch: PromiseRepoBatch) => {
          if (batch._tag === "Reset") {
            const records: VerifiedRecord[] = [];
            for await (const r of batch.records) records.push(r);
            applied.push({ tag: "Reset", records: records.map((r) => r.rkey) });
          } else {
            applied.push({ tag: batch._tag });
          }
        },
      },
    });
    try {
      await syncer.watch(space.ref);
      await syncer.awaitIdle(space.ref);
      expect(applied).toEqual([{ tag: "Reset", records: ["1"] }]);

      await space.write(alice, "com.example.post", "2", { text: "b" });
      const lxm = "com.atproto.space.notifyWrite";
      const goodAuth = `Bearer ${await net.serviceAuth(alice, { aud: SERVICE_DID, lxm })}`;
      await expect(
        syncer.notifyWrite(space.notifyWriteBody(alice.did), "Bearer nope"),
      ).rejects.toThrow();
      await syncer.notifyWrite(space.notifyWriteBody(alice.did), goodAuth);
      await syncer.awaitIdle(space.ref);
      expect(applied.at(-1)).toEqual({ tag: "Ops" });
    } finally {
      await syncer.dispose();
    }
  });
});
