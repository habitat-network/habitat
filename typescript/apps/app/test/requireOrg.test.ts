import { env } from "cloudflare:workers";
import { Effect, Exit } from "effect";
import { beforeEach, describe, expect, it } from "vitest";
import { getDb } from "@/db";
import { memberships } from "@/db/schema";
import { upsertMembershipStatement } from "@/db/memberships";
import { NotAMember } from "@/server/effect/errors";
import { CurrentSession, myOrgs, requireOrg } from "@/server/effect/member";
import { AppEnv, Database, Habitat } from "@/server/effect/services";

const ORG = "did:web:org.example";
const MEMBER = "did:plc:member";

// A deployment whose org stream answers `state` for every group, and a pear
// that lists `pearSpaces` as the member's members spaces (each one confirmed
// by a membership record unless it is in `removed`).
function run<A>(
  effect: Effect.Effect<A, unknown, any>,
  opts: { state: "live" | "behind" | "unconnected"; pearSpaces: string[] },
) {
  const fakeEnv = {
    ...env,
    ORG_STREAM: {
      idFromName: (n: string) => n,
      get: () => ({ follow: async () => opts.state }),
    },
  } as unknown as Env;
  const habitat = {
    xrpc: () =>
      Effect.succeed({
        spaces: opts.pearSpaces.map((org) => ({
          uri: `at:space/${org}/group.opensocial.members/self`,
        })),
      }),
    call: () => Effect.succeed({ value: { roles: ["member"] } }),
  };
  return Effect.runPromiseExit(
    effect.pipe(
      Effect.provideService(AppEnv, fakeEnv),
      Effect.provideService(Database, getDb(env)),
      Effect.provideService(Habitat, habitat as never),
      Effect.provideService(CurrentSession, {
        data: { did: MEMBER },
        update: () => Effect.void,
        clear: Effect.void,
      } as never),
    ) as Effect.Effect<A, unknown, never>,
  );
}

describe("a removed member of a group this deployment isn't connected to", () => {
  const db = getDb(env);
  beforeEach(async () => {
    await db.delete(memberships);
    // Synced while they were still a member; no stream ever deletes the row.
    await upsertMembershipStatement(db, {
      orgDid: ORG,
      memberDid: MEMBER,
      roles: ["member"],
    });
  });

  it("is refused by requireOrg, not let in on the stale row", async () => {
    const exit = await run(requireOrg(ORG), {
      state: "unconnected",
      pearSpaces: [],
    });
    expect(Exit.isFailure(exit)).toBe(true);
    expect(JSON.stringify(exit)).toContain(new NotAMember({ org: ORG })._tag);
  });

  it("no longer sees the group in their list", async () => {
    const exit = await run(myOrgs(), { state: "unconnected", pearSpaces: [] });
    expect(Exit.isSuccess(exit) && exit.value).toEqual([]);
  });

  it("still gets in while pear confirms they belong", async () => {
    const exit = await run(requireOrg(ORG), {
      state: "unconnected",
      pearSpaces: [ORG],
    });
    expect(Exit.isSuccess(exit)).toBe(true);
  });

  it("is let in on the synced row when the stream is live", async () => {
    const exit = await run(requireOrg(ORG), { state: "live", pearSpaces: [] });
    expect(Exit.isSuccess(exit)).toBe(true);
  });
});
