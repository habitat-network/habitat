import { DurableObject } from "cloudflare:workers";
import {
  Jetstream,
  type PutRecordInput,
  type RecordTarget,
  type Runner,
} from "@habitat-network/jetstream";
import { Cause, Effect, Exit } from "effect";
import { getDb, isConnectedOrg } from "@/db";

// OrgStream follows one org's spaces through pear's
// network.habitat.space.subscribe, with the org's own OAuth session, and
// keeps the app's D1 mirror and doc rooms in step with it. One Durable
// Object per org, so there is exactly one runner, one socket and one cursor
// per org across every isolate.
//
// It has no alarm. A request made against the org calls follow(), which
// catches D1 up over getSnapshot from the saved cursor and then keeps the
// live WebSocket open; concurrent calls share one catch-up. Each call (and
// each write) marks the object as used; after IDLE_MS without one the runner
// is stopped and the socket closed, and the next request catches up again.
// The open outbound socket is what keeps the object resident meanwhile.
//
// Writes that must be visible in D1 before a request returns go through
// putRecord/deleteRecord/awaitRecord here: the write is made with the
// writer's session, and the call returns once this object's runner has
// indexed the event it produced.

const ORG_KEY = "org";
const CURSOR_KEY = "cursor";

// How long without a request before the stream is closed.
const IDLE_MS = 5 * 60_000;

// How long a request waits for the first catch-up before carrying on with
// whatever D1 already has. The catch-up itself isn't cut short.
const CATCH_UP_WAIT_MS = 10_000;

// What follow() found: "live" once D1 is caught up to the stream's tip,
// "behind" if the catch-up failed or is still running after
// CATCH_UP_WAIT_MS (D1 may be stale), "unconnected" for an org this
// deployment holds no session for (there is no stream to follow).
export type FollowState = "live" | "behind" | "unconnected";

// What a write returns: failures come back as data, so callers don't have to
// know the SDK's tagged errors.
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
  private org: string | undefined;
  private runner: Promise<Runner> | undefined;
  // Settles once the first catch-up is applied; rejects if the stream
  // stopped before that. Set while a stream is running.
  private caughtUp: Promise<void> | undefined;
  private idleTimer: ReturnType<typeof setTimeout> | undefined;

  // follow makes sure org's stream is running, and waits (up to
  // CATCH_UP_WAIT_MS) for the catch-up. Idempotent: callers share one run.
  async follow(org: string): Promise<FollowState> {
    if (this.org && this.org !== org) {
      throw new Error(`OrgStream for ${this.org} asked to follow ${org}`);
    }
    if (!this.org) {
      this.org = org;
      await this.ctx.storage.put(ORG_KEY, org);
    }
    this.touch();
    if (!this.caughtUp) {
      if (!(await isConnectedOrg(getDb(this.env), org))) return "unconnected";
      // Another call may have started it while D1 was read.
      this.caughtUp ??= this.begin(org);
    }
    let timer: ReturnType<typeof setTimeout> | undefined;
    const timedOut = new Promise<FollowState>((resolve) => {
      timer = setTimeout(() => {
        console.warn("[orgStream] catch-up still running", org);
        resolve("behind");
      }, CATCH_UP_WAIT_MS);
    });
    try {
      return await Promise.race([
        this.caughtUp.then(
          (): FollowState => "live",
          (): FollowState => "behind",
        ),
        timedOut,
      ]);
    } finally {
      clearTimeout(timer);
    }
  }

  // alarm only clears the keepalive alarm an older version of this object
  // scheduled: it is never set again, so this runs at most once per object.
  async alarm(): Promise<void> {}

  // cursor is the seq the stream resumes from, for diagnostics and tests.
  async cursor(): Promise<number | undefined> {
    return this.ctx.storage.get<number>(CURSOR_KEY);
  }

  // running reports whether a stream is open, for tests.
  isRunning(): boolean {
    return this.caughtUp !== undefined;
  }

  private touch(): void {
    clearTimeout(this.idleTimer);
    this.idleTimer = setTimeout(() => {
      void this.stopStream();
    }, IDLE_MS);
  }

  // stopStream closes the stream now, as the idle timer does. The next
  // follow() catches up again.
  async stopStream(): Promise<void> {
    const runner = this.runner;
    this.runner = undefined;
    this.caughtUp = undefined;
    clearTimeout(this.idleTimer);
    if (runner) await (await runner).stop();
  }

  private begin(org: string): Promise<void> {
    let markCaughtUp!: () => void;
    let failCaughtUp!: (err: unknown) => void;
    const caughtUp = new Promise<void>((resolve, reject) => {
      markCaughtUp = resolve;
      failCaughtUp = reject;
    });
    caughtUp.catch(() => {}); // callers that don't wait mustn't leak a rejection
    const runner = this.buildRunner(org);
    this.runner = runner;
    runner
      .then((r) => r.replay({ snapshot: true, onCaughtUp: markCaughtUp }))
      .then(
        () => failCaughtUp(new Error("stream stopped")),
        (err: unknown) => {
          console.warn("[orgStream] stream stopped", org, err);
          failCaughtUp(err);
        },
      )
      .finally(() => {
        // Forget it (unless a newer run replaced it), so the next call
        // rebuilds the runner with a fresh session: the org may have been
        // reconnected.
        if (this.runner === runner) {
          this.runner = undefined;
          this.caughtUp = undefined;
        }
      });
    return caughtUp;
  }

  private async buildRunner(org: string): Promise<Runner> {
    // Loaded here rather than at the top so the Worker entry (which exports
    // OrgStream) doesn't pull in the sinks until a stream starts.
    const [{ appIndexer }, fetch] = await Promise.all([
      import("./appIndexer"),
      sessionFetch(this.env, org),
    ]);
    const storage = this.ctx.storage;
    const jetstream = new Jetstream({
      service: this.env.APP_HABITAT_URL,
      mode: "spaces",
      fetch,
    });
    return jetstream.runner(appIndexer(this.env), {
      // A replay is a burst of events: write them a group at a time.
      batch: { size: 50, window: "20 millis" },
      cursor: {
        get: () => storage.get<number>(CURSOR_KEY),
        set: (cursor) => storage.put(CURSOR_KEY, cursor),
      },
    });
  }

  private async write<A>(
    effect: (runner: Runner) => Effect.Effect<A, unknown>,
  ): Promise<Exit.Exit<A, unknown>> {
    this.org ??= await this.ctx.storage.get<string>(ORG_KEY);
    if (!this.org) throw new Error("OrgStream has no group yet");
    if ((await this.follow(this.org)) === "unconnected") {
      throw new Error(`${this.org} has no stream: is it connected?`);
    }
    const runner = await this.runner;
    if (!runner) throw new Error(`${this.org} has no stream: is it connected?`);
    return Effect.runPromiseExit(effect(runner));
  }

  // putRecord writes input as writer, and returns once it's indexed.
  async putRecord(
    writer: string,
    input: PutRecordInput,
  ): Promise<WriteOutcome> {
    try {
      const fetch = await sessionFetch(this.env, writer);
      const exit = await this.write((r) =>
        r.effect.putRecord(input, { fetch }),
      );
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
      const exit = await this.write((r) =>
        r.effect.deleteRecord(target, { fetch }),
      );
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
        r.effect.awaitIndexed(uri, cid ?? undefined),
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
