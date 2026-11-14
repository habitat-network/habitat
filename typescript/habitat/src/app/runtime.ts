import type { HabitatApp } from "./definition.js";
import { SliceDb, UNSAFE_ALL_SPACES } from "./db.js";
import { migrate } from "./schema.js";
import { bumpVersion, SliceSink, toLexJson, upsertRecord } from "./sink.js";
import { runBatch, type SqlDatabase } from "./sql.js";

/**
 * How the runtime calls the Habitat instance as a member: an authenticated
 * fetch, e.g. an atproto OAuth session (which is an @atproto/lex Agent).
 */
export interface HabitatAgent {
  fetchHandler(path: string, init: RequestInit): Promise<Response>;
}

export interface QueryCtx {
  /** The DID of the member the function runs as. */
  readonly viewer: string;
  /** The slice, restricted to the spaces the viewer can read. */
  readonly db: SliceDb;
  /**
   * The slice without the viewer filter. Throws unless the app's definition
   * sets unsafeCrossSpaceReads.
   */
  readonly unsafeDb: SliceDb;
}

export interface MutationCtx extends QueryCtx {
  /** Writes records to spaces as the viewer, through the Habitat instance. */
  readonly spaces: SpaceWriter;
}

/** Anything with a `parse` that validates unknown input, such as a zod schema. */
export interface ArgsParser<A> {
  parse(input: unknown): A;
}

export interface HabitatFunction<Ctx, A, R> {
  readonly kind: "query" | "mutation";
  readonly args?: ArgsParser<A> | undefined;
  readonly handler: (ctx: Ctx, args: A) => Promise<R>;
}

/**
 * query defines a read. Its handler can only read the slice, and only the
 * spaces the viewer can read.
 */
export function query<A = void, R = unknown>(def: {
  args?: ArgsParser<A>;
  handler: (ctx: QueryCtx, args: A) => Promise<R>;
}): HabitatFunction<QueryCtx, A, R> {
  return { kind: "query", args: def.args, handler: def.handler };
}

/**
 * mutation defines a write. Its handler writes through `ctx.spaces`, which
 * goes to the Habitat instance so the space's policy is enforced there.
 */
export function mutation<A = void, R = unknown>(def: {
  args?: ArgsParser<A>;
  handler: (ctx: MutationCtx, args: A) => Promise<R>;
}): HabitatFunction<MutationCtx, A, R> {
  return { kind: "mutation", args: def.args, handler: def.handler };
}

/** A write the Habitat instance refused or failed. */
export class SpaceWriteError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

export interface PutInput {
  space: string;
  collection: string;
  /** Omit to let the Habitat instance generate one. */
  rkey?: string;
  record: Record<string, unknown>;
}

export interface WrittenRecord {
  uri: string;
  cid: string;
  rkey: string;
}

/**
 * SpaceWriter writes into the viewer's own repo in a space. The Habitat
 * instance decides whether the write is allowed; once it accepts, the
 * record is also written through to the slice, so the same request reads
 * its own write. The sync stream later delivers the same record, which is
 * a no-op.
 */
export class SpaceWriter {
  constructor(
    private readonly db: SqlDatabase,
    private readonly app: HabitatApp,
    private readonly viewer: string,
    private readonly agent: HabitatAgent,
    private readonly afterWrite:
      ((space: string) => void | Promise<void>) | undefined,
  ) {}

  async put(input: PutInput): Promise<WrittenRecord> {
    this.requireCollection(input.collection);
    const record = { ...input.record, $type: input.collection };
    const body = await this.call<{ uri: string; cid: string }>(
      "com.atproto.space.putRecord",
      {
        space: input.space,
        repo: this.viewer,
        collection: input.collection,
        ...(input.rkey !== undefined ? { rkey: input.rkey } : {}),
        record,
      },
    );
    const rkey = input.rkey ?? body.uri.slice(body.uri.lastIndexOf("/") + 1);
    const uri = `${input.space}/${this.viewer}/${input.collection}/${rkey}`;
    await runBatch(this.db, [
      upsertRecord(this.db, {
        uri,
        cid: body.cid,
        space: input.space,
        repo: this.viewer,
        collection: input.collection,
        rkey,
        value: toLexJson(record),
      }),
      bumpVersion(this.db),
    ]);
    await this.afterWrite?.(input.space);
    return { uri, cid: body.cid, rkey };
  }

  async delete(input: {
    space: string;
    collection: string;
    rkey: string;
  }): Promise<void> {
    this.requireCollection(input.collection);
    await this.call("com.atproto.space.deleteRecord", {
      space: input.space,
      repo: this.viewer,
      collection: input.collection,
      rkey: input.rkey,
    });
    const uri = `${input.space}/${this.viewer}/${input.collection}/${input.rkey}`;
    await runBatch(this.db, [
      this.db.prepare(`DELETE FROM hab_records WHERE uri = ?`).bind(uri),
      bumpVersion(this.db),
    ]);
    await this.afterWrite?.(input.space);
  }

  private requireCollection(collection: string) {
    if (!(collection in this.app.definition.collections)) {
      throw new Error(`${collection} is not a collection of this app`);
    }
  }

  private async call<T>(nsid: string, body: unknown): Promise<T> {
    const res = await this.agent.fetchHandler(`/xrpc/${nsid}`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body),
    });
    if (!res.ok) {
      throw new SpaceWriteError(
        `${nsid} failed (${res.status}): ${await res.text()}`,
        res.status,
      );
    }
    return (await res.json()) as T;
  }
}

export interface RuntimeOptions {
  /** The slice's database: a D1 binding, or anything shaped like one. */
  db: SqlDatabase;
  /**
   * Returns the agent to write as for a viewer (their own session with the
   * Habitat instance). Required to run mutations.
   */
  agent?: (viewer: string) => Promise<HabitatAgent>;
  /** Called after each accepted write with its space, e.g. to start a sync pass. */
  afterWrite?: (space: string) => void | Promise<void>;
}

export interface RunOptions<A> {
  /** The DID of the signed-in member. Every function runs as someone. */
  viewer: string;
  args?: A | unknown;
}

export interface HabitatRuntime {
  /** Creates the slice's tables and the app's indexes. Safe on every start. */
  migrate(): Promise<void>;
  /** The sink a space syncer applies verified batches to. */
  readonly sink: SliceSink;
  /** Runs a query or mutation as a viewer. */
  run<Ctx extends QueryCtx, A, R>(
    fn: HabitatFunction<Ctx, A, R>,
    options: RunOptions<A>,
  ): Promise<R>;
  /** The slice's version: moves with every committed change. */
  version(): Promise<number>;
}

/**
 * createRuntime binds an app to a database. It is cheap: build one per
 * request (or per isolate) around the slice's database.
 */
export function createRuntime(
  app: HabitatApp,
  options: RuntimeOptions,
): HabitatRuntime {
  const { db } = options;
  let migrated: Promise<void> | undefined;
  const ensureMigrated = () => (migrated ??= migrate(db, app));
  return {
    migrate: ensureMigrated,
    sink: new SliceSink(db, app),
    async run(fn, { viewer, args }) {
      if (typeof viewer !== "string" || !viewer.startsWith("did:")) {
        throw new Error("a function must run as a viewer DID");
      }
      await ensureMigrated();
      const parsed = fn.args ? fn.args.parse(args) : (undefined as never);
      const queryCtx: QueryCtx = {
        viewer,
        db: new SliceDb(db, app, viewer),
        get unsafeDb(): SliceDb {
          if (!app.definition.unsafeCrossSpaceReads) {
            throw new Error(
              "unsafeDb needs unsafeCrossSpaceReads in the app definition",
            );
          }
          return new SliceDb(db, app, UNSAFE_ALL_SPACES);
        },
      };
      if (fn.kind === "query") {
        return fn.handler(queryCtx as never, parsed);
      }
      if (!options.agent) {
        throw new Error("running a mutation needs RuntimeOptions.agent");
      }
      const agent = await options.agent(viewer);
      const ctx: MutationCtx = Object.defineProperties(
        { spaces: new SpaceWriter(db, app, viewer, agent, options.afterWrite) },
        Object.getOwnPropertyDescriptors(queryCtx),
      ) as MutationCtx;
      return fn.handler(ctx as never, parsed);
    },
    async version() {
      await ensureMigrated();
      const { results } = await db
        .prepare(`SELECT value FROM hab_meta WHERE key = 'version'`)
        .all<{ value: number }>();
      return results[0]?.value ?? 0;
    },
  };
}
