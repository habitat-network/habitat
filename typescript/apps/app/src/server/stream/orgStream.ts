import { DurableObject } from "cloudflare:workers";
import { Jetstream, type Runner } from "@habitat-network/jetstream";
import { Deferred, Effect, Exit, Fiber } from "effect";
import { getDb, isConnectedOrg } from "@/db";

// OrgStream follows one org's spaces through pear's
// network.habitat.space.subscribe, with the org's own OAuth session, and
// keeps the app's D1 mirror and doc rooms in step with it. One Durable
// Object per org, so there is exactly one runner, one socket and one cursor
// per org across every isolate.
//
// It has no alarm. A request made against the org calls follow(), which
// catches D1 up over getSnapshot from the saved cursor and then keeps the
// live WebSocket open; concurrent calls share one catch-up. Each call marks
// the object as used; after IDLE_MS without one the stream is stopped and
// the socket closed, and the next request catches up again. The open
// outbound socket is what keeps the object resident meanwhile.
//
// It only follows and waits: a write is made by its caller, through the
// typed Habitat service with the writer's own session, and then awaited
// here with awaitRecord, which returns once this object's runner has indexed
// the event the write produced.

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

// A stream that is running or starting up. Everything about its progress is
// in the two Deferreds, which fail if the stream stops before they're done.
interface Run {
  readonly fiber: Fiber.Fiber<void, unknown>;
  // Completes once the first catch-up is applied.
  readonly caughtUp: Deferred.Deferred<void, unknown>;
  // The runner, once it's built (it needs the org's session first).
  readonly runner: Deferred.Deferred<Runner, unknown>;
}

export class OrgStream extends DurableObject<Env> {
  private org: string | undefined;
  private run: Run | undefined;
  private lastUsed = 0;

  // follow makes sure org's stream is running, and waits (up to
  // CATCH_UP_WAIT_MS) for the catch-up. Idempotent: callers share one run.
  async follow(org: string): Promise<FollowState> {
    if (this.org && this.org !== org) {
      throw new Error(`OrgStream for ${this.org} asked to follow ${org}`);
    }
    this.org = org;
    this.touch();
    if (!this.run) {
      if (!(await isConnectedOrg(getDb(this.env), org))) return "unconnected";
      // Another call may have started it while D1 was read.
      this.run ??= this.start(org);
    }
    const { caughtUp } = this.run;
    return Effect.runPromise(
      Deferred.await(caughtUp).pipe(
        Effect.as<FollowState>("live"),
        Effect.timeoutOrElse({
          duration: CATCH_UP_WAIT_MS,
          orElse: () => {
            console.warn("[orgStream] catch-up still running", org);
            return Effect.succeed<FollowState>("behind");
          },
        }),
        Effect.orElseSucceed((): FollowState => "behind"),
      ),
    );
  }

  // awaitRecord waits for a write the caller has made to be indexed: uri at
  // cid, or deleted when cid is null. It answers false when it wasn't
  // observed in time (the stream is behind or down: D1 catches up shortly).
  async awaitRecord(
    org: string,
    uri: string,
    cid: string | null,
  ): Promise<boolean> {
    if ((await this.follow(org)) === "unconnected") return false;
    const run = this.run;
    if (!run) return false;
    return Effect.runPromise(
      Deferred.await(run.runner).pipe(
        Effect.flatMap((runner) =>
          runner.effect.awaitIndexed(uri, cid ?? undefined),
        ),
        Effect.as(true),
        Effect.catch((err) => {
          console.warn("[orgStream] write not observed", uri, err);
          return Effect.succeed(false);
        }),
      ),
    );
  }

  // alarm only clears the keepalive alarm an older version of this object
  // scheduled: it is never set again, so this runs at most once per object.
  async alarm(): Promise<void> {}

  // cursor is the seq the stream resumes from, for diagnostics and tests.
  async cursor(): Promise<number | undefined> {
    return this.ctx.storage.get<number>(CURSOR_KEY);
  }

  // isRunning reports whether a stream is open, for tests.
  isRunning(): boolean {
    return this.run !== undefined;
  }

  // stopStream closes the stream now, as the idle timeout does. The next
  // follow() catches up again.
  async stopStream(): Promise<void> {
    const run = this.run;
    this.run = undefined;
    if (run) await Effect.runPromise(Fiber.interrupt(run.fiber));
  }

  private touch(): void {
    this.lastUsed = Date.now();
  }

  // start begins following org. Everything it needs, and everything it
  // owns (the session, the runner, the socket), lives in one fiber's scope:
  // it ends when the stream stops, fails, or goes idle.
  private start(org: string): Run {
    const caughtUp = Effect.runSync(Deferred.make<void, unknown>());
    const runner = Effect.runSync(Deferred.make<Runner, unknown>());
    const stream = this.stream(org, caughtUp, runner);
    const idle = this.untilIdle();
    const fiber = Effect.runFork(
      Effect.raceFirst(stream, idle).pipe(
        Effect.onExit((exit) =>
          Effect.sync(() => {
            if (Exit.isFailure(exit) && !Exit.hasInterrupts(exit)) {
              console.warn("[orgStream] stream stopped", org, exit.cause);
            }
            // Unfinished waiters learn the stream is over.
            const stopped = Exit.isFailure(exit)
              ? Effect.failCause(exit.cause)
              : Effect.fail(new Error("stream stopped"));
            Deferred.doneUnsafe(caughtUp, stopped);
            Deferred.doneUnsafe(runner, stopped);
            // Forget it (unless a newer run replaced it), so the next call
            // rebuilds the runner with a fresh session: the org may have
            // been reconnected.
            if (this.run?.fiber === fiber) this.run = undefined;
          }),
        ),
      ),
    );
    return { fiber, caughtUp, runner };
  }

  // untilIdle ends IDLE_MS after the last call to follow().
  private untilIdle(): Effect.Effect<void> {
    return Effect.gen({ self: this }, function* () {
      for (;;) {
        const left = this.lastUsed + IDLE_MS - Date.now();
        if (left <= 0) return;
        yield* Effect.sleep(left);
      }
    });
  }

  private stream(
    org: string,
    caughtUp: Deferred.Deferred<void, unknown>,
    ready: Deferred.Deferred<Runner, unknown>,
  ): Effect.Effect<void, unknown> {
    const { env } = this;
    const storage = this.ctx.storage;
    return Effect.gen(function* () {
      // Loaded here rather than at the top so the Worker entry (which
      // exports OrgStream) doesn't pull in the sinks until a stream starts.
      const [{ appIndexer }, fetch] = yield* Effect.all([
        Effect.promise(() => import("./appIndexer")),
        Effect.promise(async () => {
          const { createOAuthClient } = await import("../oauth");
          const session = await createOAuthClient(env).restore(org);
          return (input: string, init?: RequestInit) =>
            session.fetchHandler(input, init);
        }),
      ]);
      // Stopping the runner is the scope's finalizer, so the socket closes
      // however the stream ends: stopped, failed, or idle.
      const runner = yield* Effect.acquireRelease(
        Effect.sync(() =>
          new Jetstream({
            service: env.APP_HABITAT_URL,
            mode: "spaces",
            fetch,
          }).runner(appIndexer(env), {
            // A replay is a burst of events: write them a group at a time.
            batch: { size: 50, window: "20 millis" },
            cursor: {
              get: () => storage.get<number>(CURSOR_KEY),
              set: (cursor) => storage.put(CURSOR_KEY, cursor),
            },
          }),
        ),
        (r) => Effect.promise(() => r.stop()),
      );
      yield* Deferred.succeed(ready, runner);
      yield* Effect.tryPromise(() =>
        runner.replay({
          snapshot: true,
          onCaughtUp: () => {
            Deferred.doneUnsafe(caughtUp, Effect.void);
          },
        }),
      );
    }).pipe(Effect.scoped);
  }
}
