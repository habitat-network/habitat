import type { HabitatApp } from "./definition.js";
import { SliceDb } from "./db.js";
import {
  createRuntime,
  type HabitatAgent,
  type HabitatFunction,
  type HabitatRuntime,
  type QueryCtx,
} from "./runtime.js";
import {
  ACCESS_COLLECTION,
  MEMBERS_SPACE_TYPE,
  MEMBERSHIP_COLLECTION,
  parseSpace,
  type SliceBatch,
} from "./sink.js";
import type { SqlDatabase } from "./sql.js";

/** A write a mutation sent to the (fake) Habitat instance. */
export interface TestWrite {
  /** The viewer the write was made as. */
  viewer: string;
  nsid: string;
  body: Record<string, unknown>;
  /** Whether the instance accepted it. */
  accepted: boolean;
}

export interface TestHarness {
  readonly runtime: HabitatRuntime;
  /** Every write mutations attempted, in order. */
  readonly writes: TestWrite[];
  /** Makes `did` a member of `org` holding exactly `roles` ([] removes them). */
  member(org: string, did: string, roles: string[]): Promise<void>;
  /** Opens `space` to exactly `roles`, as its authority would. */
  access(space: string, roles: string[]): Promise<void>;
  /** Syncs a record into the slice, as if `repo` had written it to `space`. */
  record(
    space: string,
    repo: string,
    collection: string,
    rkey: string,
    value: Record<string, unknown>,
  ): Promise<void>;
  /** Syncs a record's deletion into the slice. */
  remove(
    space: string,
    repo: string,
    collection: string,
    rkey: string,
  ): Promise<void>;
  /** Applies any batch, for the cases the helpers above don't cover. */
  apply(batch: SliceBatch): Promise<void>;
  /** Runs functions as `viewer`. */
  as(viewer: string): {
    run<Ctx extends QueryCtx, A, R>(
      fn: HabitatFunction<Ctx, A, R>,
      args?: A,
    ): Promise<R>;
  };
}

/**
 * testHarness runs an app against a database without a Habitat instance, for
 * testing its functions and, above all, its permissions: seed members,
 * access and records the way sync would deliver them, then run functions as
 * different viewers and assert on what each one sees.
 *
 * Mutations go to a fake instance that decides the way the real one does: a
 * write is accepted when the viewer holds one of the roles the space is open
 * to, and refused with a 403 otherwise. Accepted and refused writes are both
 * recorded in `writes`.
 *
 * `db` is any SqlDatabase: a local D1 binding under
 * @cloudflare/vitest-pool-workers, or an in-memory SQLite.
 */
export async function testHarness(
  app: HabitatApp,
  options: { db: SqlDatabase },
): Promise<TestHarness> {
  const { db } = options;
  const writes: TestWrite[] = [];
  let nextRkey = 0;
  let nextCid = 0;

  const agent = async (viewer: string): Promise<HabitatAgent> => ({
    async fetchHandler(path, init) {
      const nsid = path.slice(path.lastIndexOf("/") + 1);
      const body = JSON.parse(String(init.body)) as Record<string, unknown>;
      const space = String(body["space"]);
      const accepted =
        body["repo"] === viewer &&
        (await new SliceDb(db, app, viewer).canRead(space));
      writes.push({ viewer, nsid, body, accepted });
      if (!accepted) {
        return Response.json(
          { error: "Forbidden", message: "viewer cannot write to this space" },
          { status: 403 },
        );
      }
      if (nsid !== "com.atproto.space.putRecord") return Response.json({});
      const rkey = body["rkey"] ?? `test${++nextRkey}`;
      return Response.json({
        uri: `${space}/${viewer}/${String(body["collection"])}/${String(rkey)}`,
        cid: `test-cid-${++nextCid}`,
      });
    },
  });

  const runtime = createRuntime(app, { db, agent });
  await runtime.migrate();

  const change = (
    space: string,
    repo: string,
    collection: string,
    rkey: string,
    value: Record<string, unknown> | undefined,
  ): SliceBatch => ({
    _tag: "Ops",
    space,
    did: repo,
    changes: [
      {
        uri: `${space}/${repo}/${collection}/${rkey}`,
        collection,
        rkey,
        cid:
          value === undefined
            ? null
            : { toString: () => `test-cid-${++nextCid}` },
        value,
      },
    ],
  });

  const authorityOf = (space: string) => {
    const parsed = parseSpace(space);
    if (!parsed) throw new Error(`${space} is not a space URI`);
    return parsed.authority;
  };

  return {
    runtime,
    writes,
    member: (org, did, roles) =>
      runtime.sink.apply(
        change(
          `at://${org}/space/${MEMBERS_SPACE_TYPE}/self`,
          org,
          MEMBERSHIP_COLLECTION,
          did,
          roles.length > 0 ? { roles } : undefined,
        ),
      ),
    access: (space, roles) =>
      runtime.sink.apply(
        change(space, authorityOf(space), ACCESS_COLLECTION, "self", {
          roles,
        }),
      ),
    record: (space, repo, collection, rkey, value) =>
      runtime.sink.apply(change(space, repo, collection, rkey, value)),
    remove: (space, repo, collection, rkey) =>
      runtime.sink.apply(change(space, repo, collection, rkey, undefined)),
    apply: (batch) => runtime.sink.apply(batch),
    as: (viewer) => ({
      run: (fn, args) => runtime.run(fn, { viewer, args }),
    }),
  };
}
