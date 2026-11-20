// @vitest-environment node
import { publicIdentityResolver } from "./Identity";
import { NOTIFY_ENDPOINT } from "./test/harness";
import { NotificationAuthError, SinkError, WireDecodeError } from "./errors";
import { notificationErrorStatus } from "./notification";
import type { VerifiedRecord } from "@atproto/space";
import { describe, expect, it } from "vitest";
import { server } from "../test/msw";
import {
  type PromiseRepoBatch,
  type PromiseSyncStore,
  createSpaceSyncer,
} from "./promise";
import { FakeNetwork, PLC_URL } from "./test/fakeNetwork";
import type { RepoState, SpaceState } from "./types";

const memoryStore = (): PromiseSyncStore => {
  const spaces = new Map<string, SpaceState>();
  const repos = new Map<string, RepoState>();
  return {
    getSpace: async (s) => spaces.get(s),
    putSpace: async (st) => void spaces.set(st.space, st),
    claimDue: async (s, seen, until) => {
      const st = spaces.get(s);
      if (st?.nextDueAt !== seen) return false;
      spaces.set(s, { ...st, nextDueAt: until });
      return true;
    },
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

describe("notificationErrorStatus", () => {
  it("retries everything but a rejected caller or a malformed body", () => {
    expect(
      notificationErrorStatus(new NotificationAuthError({ message: "no" })),
    ).toBe(401);
    expect(
      notificationErrorStatus(new WireDecodeError({ message: "bad" })),
    ).toBe(400);
    expect(
      notificationErrorStatus(
        new SinkError({ message: "db down", cause: undefined }),
      ),
    ).toBe(500);
    expect(notificationErrorStatus(new Error("boom"))).toBe(500);
  });
});

describe("createSpaceSyncer", () => {
  it("syncs through Promise ports and verifies notifications", async () => {
    const net = new FakeNetwork();
    server.use(...net.handlers);
    const alice = await net.createAccount("alice");
    const space = net.createSpace(alice);
    await space.write(alice, "com.example.post", "1", { text: "a" });

    const applied: Array<{ tag: string; records?: string[] }> = [];
    const syncer = createSpaceSyncer({
      notifyEndpoint: NOTIFY_ENDPOINT,
      notifySecret: "test-secret",
      identityResolver: publicIdentityResolver(PLC_URL),
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
      await syncer.watch(space.id);
      await syncer.awaitIdle(space.id);
      expect(applied).toEqual([{ tag: "Reset", records: ["1"] }]);

      await space.write(alice, "com.example.post", "2", { text: "b" });
      const [{ token }] = space.registrations;
      const rejected = await syncer
        .notifySpaceUpdate(space.notifySpaceUpdateBody(alice.did, "forged"))
        .then(
          () => undefined,
          (error: unknown) => error,
        );
      expect(rejected).toBeDefined();
      // A rejected caller is permanent, so the space host stops retrying.
      expect(notificationErrorStatus(rejected)).toBe(401);
      expect(
        await syncer.confirmRegistration({
          space: space.id,
          token,
          challenge: "c",
        }),
      ).toEqual({ challenge: "c" });
      await syncer.notifySpaceUpdate(
        space.notifySpaceUpdateBody(alice.did, token),
      );
      await syncer.awaitIdle(space.id);
      expect(applied.at(-1)).toEqual({ tag: "Ops" });
    } finally {
      await syncer.dispose();
    }
  });
});
