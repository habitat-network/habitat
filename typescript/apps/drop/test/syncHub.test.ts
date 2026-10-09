import { env } from "cloudflare:workers";
import type { SpaceRefString } from "@atproto/syntax";
import { describe, expect, it } from "vitest";
import { getDb } from "@/db";
import { DrizzleSyncStore } from "@/db/syncStore";
import { syncHub } from "@/server/syncHub";

describe("SyncHub", () => {
  it("runs the space syncer in the Durable Object, persisting watched spaces to D1", async () => {
    const space =
      "at://did:web:org.invalid/space/network.habitat.drop/3kfile" as SpaceRefString;
    await syncHub(env).watch(space);

    // The first pass can't reach the (nonexistent) authority, but watching
    // records the space durably either way: that row is what the scheduler
    // resumes from after the object is evicted.
    const state = await new DrizzleSyncStore(getDb(env)).getSpace(space);
    expect(state?.authority).toBe("did:web:org.invalid");
  });
});
