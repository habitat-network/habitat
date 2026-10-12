import { env } from "cloudflare:workers";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import type { SpaceState } from "internal/spaceSync";
import { beforeEach, describe, expect, it } from "vitest";
import { getDb } from "@/db";
import { syncRepos, syncSpaces } from "@/db/schema";
import { DrizzleSyncStore } from "@/db/syncStore";

const SPACE =
  "at://did:web:org.example/space/network.habitat.drop/a" as SpaceRefString;
const OTHER =
  "at://did:web:org.example/space/network.habitat.drop/b" as SpaceRefString;
const ORG = "did:web:org.example" as DidString;
const ALICE = "did:plc:alice" as DidString;

const space = (over: Partial<SpaceState> = {}): SpaceState => ({
  space: SPACE,
  authority: ORG,
  nextDueAt: 1_000,
  failures: 0,
  ...over,
});

describe("DrizzleSyncStore", () => {
  const db = getDb(env);
  const store = new DrizzleSyncStore(db);

  beforeEach(async () => {
    await db.delete(syncRepos);
    await db.delete(syncSpaces);
  });

  it("round-trips a space, including unset optional fields", async () => {
    expect(await store.getSpace(SPACE)).toBeUndefined();
    await store.putSpace(space());
    expect(await store.getSpace(SPACE)).toEqual({
      space: SPACE,
      authority: ORG,
      spaceRev: undefined,
      registrationExpiresAt: undefined,
      nextDueAt: 1_000,
      lastFullPassAt: undefined,
      failures: 0,
      lastError: undefined,
    });

    await store.putSpace(
      space({ spaceRev: "3abc", failures: 2, lastError: "boom" }),
    );
    expect(await store.getSpace(SPACE)).toMatchObject({
      spaceRev: "3abc",
      failures: 2,
      lastError: "boom",
    });
  });

  it("claimDue is a compare-and-set on nextDueAt", async () => {
    await store.putSpace(space({ nextDueAt: 1_000 }));
    expect(await store.claimDue(SPACE, 999, 5_000)).toBe(false);
    expect(await store.claimDue(SPACE, 1_000, 5_000)).toBe(true);
    // The first claim moved it on, so a second claimer that saw the old
    // value loses.
    expect(await store.claimDue(SPACE, 1_000, 6_000)).toBe(false);
    expect((await store.getSpace(SPACE))?.nextDueAt).toBe(5_000);
  });

  it("dueSpaces returns only due spaces, oldest first, up to limit", async () => {
    await store.putSpace(space({ space: SPACE, nextDueAt: 2_000 }));
    await store.putSpace(space({ space: OTHER, nextDueAt: 1_000 }));
    const third = `${SPACE}x` as SpaceRefString;
    await store.putSpace(space({ space: third, nextDueAt: 9_000 }));

    const due = await store.dueSpaces(5_000, 10);
    expect(due.map((s) => s.space)).toEqual([OTHER, SPACE]);
    expect((await store.dueSpaces(5_000, 1)).map((s) => s.space)).toEqual([
      OTHER,
    ]);
  });

  it("stores repo state and copies the LtHash", async () => {
    const ltHash = new Uint8Array(2048).fill(7);
    await store.putRepo({ space: SPACE, did: ALICE, rev: "r1", ltHash });
    ltHash.fill(0);

    const repo = await store.getRepo(SPACE, ALICE);
    expect(repo?.rev).toBe("r1");
    expect(repo?.ltHash).toEqual(new Uint8Array(2048).fill(7));
    expect(await store.listRepoDids(SPACE)).toEqual([ALICE]);

    await store.putRepo({
      space: SPACE,
      did: ALICE,
      rev: "r2",
      ltHash: new Uint8Array(2048),
    });
    expect((await store.getRepo(SPACE, ALICE))?.rev).toBe("r2");

    await store.removeRepo(SPACE, ALICE);
    expect(await store.getRepo(SPACE, ALICE)).toBeUndefined();
  });

  it("removeSpace also drops the space's repos, and only its own", async () => {
    await store.putSpace(space({ space: SPACE }));
    await store.putSpace(space({ space: OTHER }));
    const ltHash = new Uint8Array(2048);
    await store.putRepo({ space: SPACE, did: ALICE, rev: "r", ltHash });
    await store.putRepo({ space: OTHER, did: ALICE, rev: "r", ltHash });

    await store.removeSpace(SPACE);
    expect(await store.getSpace(SPACE)).toBeUndefined();
    expect(await store.listRepoDids(SPACE)).toEqual([]);
    expect(await store.getSpace(OTHER)).toBeDefined();
    expect(await store.listRepoDids(OTHER)).toEqual([ALICE]);
  });
});
