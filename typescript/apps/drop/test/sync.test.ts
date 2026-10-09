import { env } from "cloudflare:workers";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import { beforeEach, describe, expect, it } from "vitest";
import { getDb } from "@/db";
import { syncRepos, syncSpaces } from "@/db/schema";
import { DrizzleSyncStore } from "@/db/syncStore";
import { notifyWrite, runDueSpaces } from "@/server/sync";

// The authority doesn't exist, so every pass here fails; what these check
// is that the syncer is started, claims, and records its outcome in D1.
const ORG = "did:web:org.invalid" as DidString;
const SPACE = `at://${ORG}/space/network.habitat.drop/3kfile` as SpaceRefString;

describe("sync", () => {
  const store = new DrizzleSyncStore(getDb(env));

  beforeEach(async () => {
    await getDb(env).delete(syncRepos);
    await getDb(env).delete(syncSpaces);
  });

  it("runDueSpaces runs the passes that are due and records the outcome", async () => {
    await store.putSpace({
      space: SPACE,
      authority: ORG,
      nextDueAt: Date.now() - 1_000,
      failures: 0,
    });

    const started = Date.now();
    await runDueSpaces(env);

    const state = await store.getSpace(SPACE);
    expect(state?.failures).toBe(1);
    expect(state?.lastError).toBeTruthy();
    expect(state?.nextDueAt).toBeGreaterThan(started);
  });

  it("runDueSpaces leaves spaces that aren't due alone", async () => {
    const later = Date.now() + 60_000;
    await store.putSpace({
      space: SPACE,
      authority: ORG,
      nextDueAt: later,
      failures: 0,
    });
    await runDueSpaces(env);
    expect((await store.getSpace(SPACE))?.nextDueAt).toBe(later);
  });

  it("ignores notifications for spaces Drop doesn't sync", async () => {
    await notifyWrite(
      env,
      {
        space: SPACE,
        repo: ORG,
        repoRev: "3kabcdefghi22",
        hash: { $bytes: "AAAA" },
      },
      undefined,
    );
    expect(await store.getSpace(SPACE)).toBeUndefined();
  });
});
