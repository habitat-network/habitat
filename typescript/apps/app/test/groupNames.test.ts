import { env } from "cloudflare:test";
import { Effect, Layer } from "effect";
import { beforeEach, describe, expect, it } from "vitest";
import { getDb, upsertConnectedOrg } from "@/db";
import { connectedOrgs } from "@/db/schema";
import {
  Database,
  Habitat,
  type HabitatService,
} from "@/server/effect/services";
import { groupNames } from "@/server/groups";

const ORG = "did:web:org.example";
const MEMBER = "did:plc:alice";

// A Habitat whose profile record holds `profileName`.
function habitatWith(profileName: string | null): HabitatService {
  return {
    call: () =>
      profileName === null
        ? Effect.die("profile unreadable")
        : Effect.succeed({ value: { displayName: profileName } }),
  } as unknown as HabitatService;
}

const run = (profileName: string | null) =>
  Effect.runPromise(
    groupNames(MEMBER, [ORG]).pipe(
      Effect.provide(
        Layer.mergeAll(
          Layer.succeed(Database, getDb(env)),
          Layer.succeed(Habitat, habitatWith(profileName)),
        ),
      ),
    ),
  );

beforeEach(async () => {
  await getDb(env).delete(connectedOrgs);
  await upsertConnectedOrg(getDb(env), {
    orgDid: ORG,
    name: "Old name",
    connectedBy: MEMBER,
  });
});

describe("groupNames", () => {
  it("shows a renamed connected group's current profile name, not the name cached at connect time", async () => {
    const names = await run("New name");
    expect(names.get(ORG)).toEqual({ name: "New name", connected: true });
  });

  it("falls back to the cached name when the profile has none", async () => {
    const names = await Effect.runPromise(
      groupNames(MEMBER, [ORG]).pipe(
        Effect.provide(
          Layer.mergeAll(
            Layer.succeed(Database, getDb(env)),
            Layer.succeed(Habitat, {
              call: () => Effect.succeed({ value: {} }),
            } as unknown as HabitatService),
          ),
        ),
      ),
    );
    expect(names.get(ORG)).toEqual({ name: "Old name", connected: true });
  });
});
