import { DurableObject } from "cloudflare:workers";
import {
  Jetstream,
  type PutRecordInput,
  type RecordTarget,
  type Runner,
} from "@habitat-network/jetstream";
import { Cause, Effect, Exit } from "effect";
import { appIndexer } from "./indexer";

// OrgStream follows one group's spaces through pear's
// network.habitat.space.subscribe, with the group's own OAuth session, and
// keeps the app's D1 mirror and doc rooms in step with it. One Durable
// Object per group, so each group's events are applied by exactly one
// runner, in order, with one cursor for the whole group.
//
// Writes that must be visible in D1 before a request returns go through
// putRecord/deleteRecord/awaitRecord here: the write is made with the
// writer's session, and the call returns once this object's runner has
// indexed the event it produced.

const ORG_KEY = "org";
const CURSOR_KEY = "cursor";

// While following, the object sets itself an alarm this often. It restarts
// a runner that stopped (for instance because the group's session was
// refused) and keeps the object, and so its outbound stream, alive.
const KEEPALIVE_MS = 30_000;

// What a write returns across the RPC boundary: Effect's tagged errors
// don't survive serialization, so failures come back as data.
export type WriteOutcome =
  | { ok: true; uri: string; cid: string }
  | {
      ok: false;
      tag: "WriteError" | "WriteNotObserved" | "Unavailable";
      status?: number;
      error?: string;
      message: string;
    };

type Failure = Extract<WriteOutcome, { ok: false }>;

function failureOf(err: unknown): Failure {
  const e = err as {
    _tag?: string;
    status?: number;
    error?: string;
    message?: string;
    uri?: string;
  };
  if (e?._tag === "WriteError") {
    return {
      ok: false,
      tag: "WriteError",
      status: e.status,
      error: e.error,
      message: e.message ?? "write failed",
    };
  }
  if (e?._tag === "WriteNotObserved") {
    return {
      ok: false,
      tag: "WriteNotObserved",
      message: `${e.uri} was written but isn't indexed yet`,
    };
  }
  return {
    ok: false,
    tag: "Unavailable",
    message: err instanceof Error ? err.message : String(err),
  };
}

async function sessionFetch(env: Env, did: string) {
  const { createOAuthClient } = await import("../oauth");
  const session = await createOAuthClient(env).restore(did);
  return (input: string, init?: RequestInit) =>
    session.fetchHandler(input, init);
}

export class OrgStream extends DurableObject<Env> {
  private runner: Runner | undefined;
  private org: string | undefined;

  // ensure starts following org's spaces, if this object isn't already.
  async ensure(org: string): Promise<void> {
    if (this.org && this.org !== org) {
      throw new Error(`OrgStream for ${this.org} asked to follow ${org}`);
    }
    if (!this.org) {
      this.org = org;
      await this.ctx.storage.put(ORG_KEY, org);
    }
    await this.start();
    if ((await this.ctx.storage.getAlarm()) === null) {
      await this.ctx.storage.setAlarm(Date.now() + KEEPALIVE_MS);
    }
  }

  async alarm(): Promise<void> {
    this.org ??= await this.ctx.storage.get<string>(ORG_KEY);
    if (!this.org) return;
    await this.start();
    await this.ctx.storage.setAlarm(Date.now() + KEEPALIVE_MS);
  }

  // cursor is the seq the stream resumes from, for diagnostics and tests.
  async cursor(): Promise<number | undefined> {
    return this.ctx.storage.get<number>(CURSOR_KEY);
  }

  private async getRunner(): Promise<Runner> {
    if (this.runner) return this.runner;
    const org = this.org;
    if (!org) throw new Error("OrgStream has no group yet");
    const [{ AppSink }, fetch] = await Promise.all([
      import("./appSink"),
      sessionFetch(this.env, org),
    ]);
    const storage = this.ctx.storage;
    const jetstream = new Jetstream({
      service: this.env.APP_HABITAT_URL,
      mode: "spaces",
      fetch,
    });
    this.runner = jetstream.runner(appIndexer(AppSink.for(this.env)), {
      cursor: {
        get: () => storage.get<number>(CURSOR_KEY),
        set: (cursor) => storage.put(CURSOR_KEY, cursor),
      },
    });
    return this.runner;
  }

  private async start(): Promise<void> {
    const runner = await this.getRunner();
    if (runner.running()) return;
    // A fresh group starts at 0: the compacted log replays every record
    // the group can read, then the stream follows it live.
    runner.replay().catch((err: unknown) => {
      console.warn("[orgStream] stream stopped", this.org, err);
      // Forget the runner, so the next alarm rebuilds it with a fresh
      // session (the group may have been reconnected).
      this.runner = undefined;
    });
  }

  private async write<A>(
    effect: (runner: Runner) => Effect.Effect<A, unknown>,
  ): Promise<Exit.Exit<A, unknown>> {
    if (!this.org) this.org = await this.ctx.storage.get<string>(ORG_KEY);
    await this.start();
    const runner = await this.getRunner();
    return Effect.runPromiseExit(effect(runner));
  }

  // putRecord writes input as writer, and returns once it's indexed.
  async putRecord(
    writer: string,
    input: PutRecordInput,
  ): Promise<WriteOutcome> {
    try {
      const fetch = await sessionFetch(this.env, writer);
      const exit = await this.write((r) => r.putRecord(input, { fetch }));
      if (Exit.isSuccess(exit)) return { ok: true, ...exit.value };
      return failureOf(squashed(exit));
    } catch (err) {
      return failureOf(err);
    }
  }

  // deleteRecord deletes target as writer, and returns once it's indexed.
  async deleteRecord(
    writer: string,
    target: RecordTarget,
  ): Promise<WriteOutcome> {
    try {
      const fetch = await sessionFetch(this.env, writer);
      const exit = await this.write((r) => r.deleteRecord(target, { fetch }));
      if (Exit.isSuccess(exit)) return { ok: true, uri: "", cid: "" };
      return failureOf(squashed(exit));
    } catch (err) {
      return failureOf(err);
    }
  }

  // awaitRecord waits for a write made some other way (a pear procedure
  // that writes records itself) to be indexed: uri with cid, or deleted
  // when cid is null.
  async awaitRecord(uri: string, cid: string | null): Promise<WriteOutcome> {
    try {
      const exit = await this.write((r) =>
        r.awaitIndexed(uri, cid ?? undefined),
      );
      if (Exit.isSuccess(exit)) return { ok: true, uri, cid: cid ?? "" };
      return failureOf(squashed(exit));
    } catch (err) {
      return failureOf(err);
    }
  }
}

function squashed(exit: Exit.Exit<unknown, unknown>): unknown {
  return Exit.isFailure(exit) ? Cause.squash(exit.cause) : undefined;
}
