import { waitUntil } from "cloudflare:workers";
import {
  Jetstream,
  type PutRecordInput,
  type RecordTarget,
  type Runner,
} from "@habitat-network/jetstream";
import { Cause, Effect, Exit } from "effect";
import {
  getDb,
  isConnectedOrg,
  orgStreamCursor,
  setOrgStreamCursor,
} from "@/db";
import { appEventSink, INDEXED_COLLECTIONS } from "./indexer";

// An org's stream follows its spaces through pear's
// network.habitat.space.subscribe, with the org's own OAuth session, and
// keeps the app's D1 mirror and doc rooms in step with it.
//
// There is no long-lived object behind it. A request made against an org
// calls followOrg, which (once per isolate per org) catches the mirror up
// over getSnapshot's HTTP pages from the cursor saved in D1, then keeps the
// live WebSocket open for as long as the isolate stays awake. When the
// isolate goes idle and is evicted the socket goes with it; the next request
// to that org catches up again from the saved cursor.
//
// Writes that must be visible in D1 before a request returns go through
// putRecord/deleteRecord/awaitRecord here: the write is made with the
// writer's session, and the call returns once this isolate's runner has
// indexed the event it produced.

// How long a request waits for the first catch-up before carrying on with
// whatever D1 already has. The catch-up itself isn't cut short.
const CATCH_UP_WAIT_MS = 10_000;

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

interface Following {
  runner: Promise<Runner>;
  // Settles once the org's mirror is caught up to the stream's tip and the
  // socket is opening; rejects if the stream stopped before getting there.
  caughtUp: Promise<void>;
}

// The streams this isolate is running, by org. An entry is added
// synchronously, before anything is awaited, so concurrent requests for one
// org share a single catch-up and a single socket. It's removed when the
// stream stops, so the next request starts it again.
const following = new Map<string, Following>();

function start(env: Env, org: string): Following {
  let markCaughtUp!: () => void;
  let failCaughtUp!: (err: unknown) => void;
  const caughtUp = new Promise<void>((resolve, reject) => {
    markCaughtUp = resolve;
    failCaughtUp = reject;
  });
  caughtUp.catch(() => {}); // callers that don't wait mustn't leak a rejection

  const runner = buildRunner(env, org);
  const entry: Following = { runner, caughtUp };
  const done = runner
    .then((r) => r.replay({ snapshot: true, onCaughtUp: markCaughtUp }))
    .then(
      () => failCaughtUp(new Error("stream stopped")),
      (err: unknown) => {
        console.warn("[orgStream] stream stopped", org, err);
        failCaughtUp(err);
      },
    )
    .finally(() => {
      // Forget the runner, so the next request rebuilds it with a fresh
      // session (the org may have been reconnected).
      if (following.get(org) === entry) following.delete(org);
    });
  // Keeps the stream going for as long as the runtime allows past the
  // request that started it.
  waitUntil(done);
  return entry;
}

async function buildRunner(env: Env, org: string): Promise<Runner> {
  const [{ AppSink }, fetch] = await Promise.all([
    import("./appSink"),
    sessionFetch(env, org),
  ]);
  const db = getDb(env);
  const jetstream = new Jetstream({
    service: env.APP_HABITAT_URL,
    mode: "spaces",
    fetch,
  });
  return jetstream.runner(appEventSink(AppSink.for(env)), {
    collections: INDEXED_COLLECTIONS,
    // A replay is a burst of events: write them a group at a time.
    batch: { size: 50, window: "20 millis" },
    // The cursor lives in D1, shared by every isolate; it only moves forward.
    cursor: {
      get: () => orgStreamCursor(db, org),
      set: (cursor) => setOrgStreamCursor(db, org, cursor),
    },
  });
}

// followOrg makes sure this isolate is following org's spaces, and waits (up
// to CATCH_UP_WAIT_MS) for the first catch-up. Call it on a request made
// against org. It does nothing for an org that isn't connected.
export async function followOrg(env: Env, org: string): Promise<void> {
  let entry = following.get(org);
  if (!entry) {
    if (!(await isConnectedOrg(getDb(env), org))) return;
    // Another request may have started it while D1 was read.
    entry = following.get(org) ?? start(env, org);
    following.set(org, entry);
  }
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timedOut = new Promise<void>((resolve) => {
    timer = setTimeout(() => {
      console.warn("[orgStream] catch-up still running", org);
      resolve();
    }, CATCH_UP_WAIT_MS);
  });
  try {
    await Promise.race([entry.caughtUp, timedOut]);
  } finally {
    clearTimeout(timer);
  }
}

// stopFollowing stops every stream this isolate is running (for tests).
export async function stopFollowing(): Promise<void> {
  const all = [...following.values()];
  following.clear();
  await Promise.all(all.map((f) => f.runner.then((r) => r.stop())));
}

async function write<A>(
  env: Env,
  org: string,
  effect: (runner: Runner) => Effect.Effect<A, unknown>,
): Promise<Exit.Exit<A, unknown>> {
  await followOrg(env, org);
  const entry = following.get(org);
  if (!entry) throw new Error(`${org} has no stream: is it connected?`);
  return Effect.runPromiseExit(effect(await entry.runner));
}

function squashed(exit: Exit.Exit<unknown, unknown>): unknown {
  return Exit.isFailure(exit) ? Cause.squash(exit.cause) : undefined;
}

// putRecord writes input as writer, and returns once it's indexed.
export async function putRecord(
  env: Env,
  org: string,
  writer: string,
  input: PutRecordInput,
): Promise<WriteOutcome> {
  try {
    const fetch = await sessionFetch(env, writer);
    const exit = await write(env, org, (r) => r.putRecord(input, { fetch }));
    if (Exit.isSuccess(exit)) return { ok: true, ...exit.value };
    return failureOf(squashed(exit));
  } catch (err) {
    return failureOf(err);
  }
}

// deleteRecord deletes target as writer, and returns once it's indexed.
export async function deleteRecord(
  env: Env,
  org: string,
  writer: string,
  target: RecordTarget,
): Promise<WriteOutcome> {
  try {
    const fetch = await sessionFetch(env, writer);
    const exit = await write(env, org, (r) =>
      r.deleteRecord(target, { fetch }),
    );
    if (Exit.isSuccess(exit)) return { ok: true, uri: "", cid: "" };
    return failureOf(squashed(exit));
  } catch (err) {
    return failureOf(err);
  }
}

// awaitRecord waits for a write made some other way (a pear procedure that
// writes records itself) to be indexed: uri with cid, or deleted when cid is
// null.
export async function awaitRecord(
  env: Env,
  org: string,
  uri: string,
  cid: string | null,
): Promise<WriteOutcome> {
  try {
    const exit = await write(env, org, (r) =>
      r.awaitIndexed(uri, cid ?? undefined),
    );
    if (Exit.isSuccess(exit)) return { ok: true, uri, cid: cid ?? "" };
    return failureOf(squashed(exit));
  } catch (err) {
    return failureOf(err);
  }
}
