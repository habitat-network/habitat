import { lexStringify } from "@atproto/lex-json";
import {
  Cause,
  Deferred,
  Duration,
  Effect,
  Fiber,
  Option,
  Schedule,
  Stream,
} from "effect";
import {
  CursorError,
  FrameDecodeError,
  HandlerError,
  StreamError,
  WriteError,
  WriteNotObserved,
  errorMessage,
} from "./errors";
import type { CommitEvent, SpaceEvent } from "./events";
import { toEffect } from "./handler";
import type { Jetstream } from "./Jetstream";
import type { LexIndexer } from "./LexIndexer";
import type { FetchHandler } from "./transport";

/**
 * Where the runner keeps its cursor: the seq to resume from, which is one
 * past the last event every handler finished. One value for the whole
 * stream, however many spaces and repos it spans.
 */
export interface CursorStore {
  get():
    | Effect.Effect<number | undefined, unknown>
    | PromiseLike<number | undefined>
    | number
    | undefined;
  set(cursor: number): Effect.Effect<void, unknown> | PromiseLike<void> | void;
}

export interface RunnerOptions {
  /** Persists the cursor. Without one the runner keeps it in memory. */
  readonly cursor?: CursorStore;
  /** How long a write waits for its event to be indexed. Default 15s. */
  readonly writeTimeout?: Duration.Input;
  /** How many recently indexed records are remembered for late waiters. */
  readonly recentLimit?: number;
}

export interface ReplayOptions {
  /**
   * The seq to start from, inclusive. Defaults to the cursor store's, else
   * 0: a fresh runner replays the (compacted) log and converges on the
   * current state before following it live.
   */
  readonly cursor?: number;
}

/** Selects the record a write goes to. */
export interface RecordTarget {
  readonly space: string;
  /** The repo (author) DID: the account `fetch` writes as. */
  readonly repo: string;
  readonly collection: string;
  readonly rkey: string;
}

export interface PutRecordInput extends RecordTarget {
  readonly record: unknown;
}

export interface CreateRecordInput extends Omit<RecordTarget, "rkey"> {
  readonly rkey?: string;
  readonly record: unknown;
}

export interface WriteOptions {
  /**
   * The writer's credential: their OAuth session's fetchHandler, which
   * resolves the XRPC path against their own host (pear, or their PDS).
   * Defaults to the Jetstream's own (the org's).
   */
  readonly fetch?: FetchHandler;
  /** Overrides RunnerOptions.writeTimeout for this write. */
  readonly timeout?: Duration.Input;
}

export interface WriteResult {
  readonly uri: string;
  readonly cid: string;
}

type Indexed = { readonly cid: string | undefined };

interface Waiter {
  readonly cid: string | undefined;
  readonly done: Deferred.Deferred<void>;
}

const HANDLER_RETRY = Schedule.exponential("200 millis").pipe(
  Schedule.modifyDelay((m) =>
    Effect.succeed(Duration.min(m.duration, Duration.minutes(1))),
  ),
);

/**
 * Runner runs a LexIndexer's handlers over a Jetstream, one event at a
 * time, and advances the cursor only after an event's handlers all succeed:
 * a failing handler is retried with backoff rather than skipped, so the
 * index never silently misses a write.
 *
 * Its write methods (putRecord, createRecord, deleteRecord) make the write,
 * then wait until the runner has indexed the event it produced, so a caller
 * that awaits one can read its own write back from the index and needs no
 * optimistic update. The event is matched on its record URI and CID (CIDs
 * hash the record's content, so the URI is what tells two equal records
 * apart); a delete on its URI.
 */
export class Runner<E = unknown> {
  private readonly waiters = new Map<string, Waiter[]>();
  // The latest event indexed per record URI (insertion-ordered, oldest
  // evicted first), for a write whose event is indexed before its own
  // response arrives.
  private readonly recent = new Map<string, Indexed>();
  private readonly recentLimit: number;
  private readonly writeTimeout: Duration.Duration;
  private memoryCursor: number | undefined;
  private fiber: Fiber.Fiber<void, unknown> | undefined;

  constructor(
    readonly jetstream: Jetstream,
    readonly indexer: LexIndexer<E>,
    readonly options: RunnerOptions = {},
  ) {
    this.recentLimit = options.recentLimit ?? 10_000;
    this.writeTimeout = Duration.fromInputUnsafe(
      options.writeTimeout ?? "15 seconds",
    );
  }

  /** The cursor the runner would resume from now. */
  cursor(): Effect.Effect<number | undefined, CursorError> {
    const store = this.options.cursor;
    if (!store) return Effect.succeed(this.memoryCursor);
    return toEffect(() => store.get()).pipe(
      Effect.mapError(
        (cause) =>
          new CursorError({
            message: `read cursor: ${errorMessage(cause)}`,
            cause,
          }),
      ),
    );
  }

  private saveCursor(cursor: number): Effect.Effect<void, CursorError> {
    this.memoryCursor = cursor;
    const store = this.options.cursor;
    if (!store) return Effect.void;
    return toEffect(() => store.set(cursor)).pipe(
      Effect.mapError(
        (cause) =>
          new CursorError({
            message: `save cursor: ${errorMessage(cause)}`,
            cause,
          }),
      ),
    );
  }

  /**
   * run indexes the stream from the cursor, then follows it live, until
   * interrupted or the stream fails in a way reconnecting can't fix.
   */
  run(
    options: ReplayOptions = {},
  ): Effect.Effect<void, StreamError | FrameDecodeError | CursorError> {
    const self = this;
    return Effect.gen(function* () {
      const cursor = options.cursor ?? (yield* self.cursor()) ?? 0;
      yield* self.jetstream
        .subscribe({ cursor, collections: self.indexer.collections() })
        .pipe(Stream.runForEach((event) => self.apply(event)));
    });
  }

  /** apply indexes one event and settles the writes waiting on it. */
  apply(event: SpaceEvent): Effect.Effect<void, CursorError> {
    const self = this;
    return Effect.gen(function* () {
      yield* self.indexer.handle(event).pipe(
        Effect.mapError(
          (cause) =>
            new HandlerError({
              uri: event.kind === "spaceDeleted" ? event.space : event.uri,
              message: errorMessage(cause),
              cause,
            }),
        ),
        Effect.tapError((err) =>
          Effect.logWarning("jetstream: handler failed; retrying", {
            uri: err.uri,
            error: err.message,
          }),
        ),
        // The schedule never ends, so nothing is left to fail with.
        Effect.retry(HANDLER_RETRY),
        Effect.orDie,
      );
      yield* self.saveCursor(event.seq + 1);
      if (event.kind !== "spaceDeleted") self.indexed(event);
    });
  }

  // indexed records event as the latest indexed for its record, and wakes
  // the writes it settles. Synchronous, so a write registering in between
  // can't miss it.
  private indexed(event: CommitEvent) {
    const cid = event.kind === "put" ? event.cid : undefined;
    this.recent.delete(event.uri);
    this.recent.set(event.uri, { cid });
    if (this.recent.size > this.recentLimit) {
      const oldest = this.recent.keys().next();
      if (!oldest.done) this.recent.delete(oldest.value);
    }
    const waiting = this.waiters.get(event.uri);
    if (!waiting) return;
    const remaining = waiting.filter((w) => {
      if (w.cid !== cid) return true;
      Deferred.doneUnsafe(w.done, Effect.void);
      return false;
    });
    if (remaining.length > 0) this.waiters.set(event.uri, remaining);
    else this.waiters.delete(event.uri);
  }

  /**
   * awaitIndexed waits until the record at uri has been indexed with cid
   * (undefined: deleted), or fails with WriteNotObserved after the timeout.
   */
  awaitIndexed(
    uri: string,
    cid: string | undefined,
    timeout: Duration.Input = this.writeTimeout,
  ): Effect.Effect<void, WriteNotObserved> {
    return Effect.suspend(() => {
      const latest = this.recent.get(uri);
      if (latest && latest.cid === cid) return Effect.void;
      const done = Deferred.makeUnsafe<void>();
      const waiter: Waiter = { cid, done };
      this.waiters.set(uri, [...(this.waiters.get(uri) ?? []), waiter]);
      const forget = Effect.sync(() => {
        const left = (this.waiters.get(uri) ?? []).filter((w) => w !== waiter);
        if (left.length > 0) this.waiters.set(uri, left);
        else this.waiters.delete(uri);
      });
      return Deferred.await(done).pipe(
        Effect.timeoutOrElse({
          duration: timeout,
          orElse: () => Effect.fail(new WriteNotObserved({ uri, cid })),
        }),
        Effect.ensuring(forget),
      );
    });
  }

  private xrpc(
    nsid: string,
    method: "GET" | "POST",
    params: Record<string, unknown>,
    fetch: FetchHandler,
  ): Effect.Effect<unknown, WriteError> {
    return Effect.gen(function* () {
      let path = `/xrpc/${nsid}`;
      const init: RequestInit = { method };
      if (method === "GET") {
        const qs = new URLSearchParams();
        for (const [k, v] of Object.entries(params)) {
          if (v !== undefined) qs.set(k, String(v));
        }
        path = `${path}?${qs}`;
      } else {
        init.headers = { "content-type": "application/json" };
        init.body = lexStringify(params as Parameters<typeof lexStringify>[0]);
      }
      const res = yield* Effect.tryPromise({
        try: () => fetch(path, init),
        catch: (cause) =>
          new WriteError({ nsid, status: 0, message: errorMessage(cause) }),
      });
      const text = yield* Effect.promise(() => res.text().catch(() => ""));
      let body: unknown;
      try {
        body = text ? JSON.parse(text) : undefined;
      } catch {
        body = undefined;
      }
      if (!res.ok) {
        const error =
          typeof (body as { error?: unknown })?.error === "string"
            ? (body as { error: string }).error
            : undefined;
        const message =
          typeof (body as { message?: unknown })?.message === "string"
            ? (body as { message: string }).message
            : text;
        return yield* new WriteError({
          nsid,
          status: res.status,
          ...(error ? { error } : {}),
          message,
        });
      }
      return body;
    });
  }

  private writeResult(
    nsid: string,
    body: unknown,
  ): Effect.Effect<WriteResult, WriteError> {
    const { uri, cid } = (body ?? {}) as { uri?: unknown; cid?: unknown };
    if (typeof uri !== "string" || typeof cid !== "string") {
      return Effect.fail(
        new WriteError({
          nsid,
          status: 200,
          message: "response has no uri and cid",
        }),
      );
    }
    return Effect.succeed({ uri, cid });
  }

  /**
   * putRecord writes a record with com.atproto.space.putRecord, then waits
   * until the runner has indexed it. A WriteError means nothing was written;
   * WriteNotObserved means it was, but isn't indexed yet.
   */
  putRecord(
    input: PutRecordInput,
    options: WriteOptions = {},
  ): Effect.Effect<WriteResult, WriteError | WriteNotObserved> {
    const nsid = "com.atproto.space.putRecord";
    return this.xrpc(
      nsid,
      "POST",
      { ...input },
      options.fetch ?? this.jetstream.fetch,
    ).pipe(
      Effect.flatMap((body) => this.writeResult(nsid, body)),
      Effect.tap(({ uri, cid }) =>
        this.awaitIndexed(uri, cid, options.timeout),
      ),
    );
  }

  /** createRecord is putRecord for com.atproto.space.createRecord. */
  createRecord(
    input: CreateRecordInput,
    options: WriteOptions = {},
  ): Effect.Effect<WriteResult, WriteError | WriteNotObserved> {
    const nsid = "com.atproto.space.createRecord";
    return this.xrpc(
      nsid,
      "POST",
      { ...input },
      options.fetch ?? this.jetstream.fetch,
    ).pipe(
      Effect.flatMap((body) => this.writeResult(nsid, body)),
      Effect.tap(({ uri, cid }) =>
        this.awaitIndexed(uri, cid, options.timeout),
      ),
    );
  }

  /**
   * deleteRecord deletes a record with com.atproto.space.deleteRecord, then
   * waits until the runner has indexed the delete. Deleting a record that
   * doesn't exist produces no event, so it's looked up first, and returns
   * at once when it's already gone.
   */
  deleteRecord(
    input: RecordTarget,
    options: WriteOptions = {},
  ): Effect.Effect<void, WriteError | WriteNotObserved> {
    const fetch = options.fetch ?? this.jetstream.fetch;
    const self = this;
    return Effect.gen(function* () {
      const found = yield* self
        .xrpc("com.atproto.space.getRecord", "GET", { ...input }, fetch)
        .pipe(
          Effect.map((body) =>
            Option.fromNullishOr((body as { uri?: string })?.uri),
          ),
          Effect.catchIf(
            (e) => e.status === 404 || e.error === "RecordNotFound",
            () => Effect.succeed(Option.none<string>()),
          ),
        );
      yield* self.xrpc(
        "com.atproto.space.deleteRecord",
        "POST",
        { ...input },
        fetch,
      );
      if (Option.isNone(found)) return;
      yield* self.awaitIndexed(found.value, undefined, options.timeout);
    });
  }

  /**
   * replay starts the runner in the background, like @bsky/jetstream's
   * `js.runner(indexer).replay({ cursor })`. The returned promise settles
   * when the runner stops: with stop(), or on an error it can't recover
   * from.
   */
  replay(options: ReplayOptions = {}): Promise<void> {
    if (this.fiber) throw new Error("runner already started");
    const fiber = Effect.runFork(this.run(options));
    this.fiber = fiber;
    // Stopping interrupts the fiber; that's a clean end, not a failure.
    return Effect.runPromise(
      Fiber.join(fiber).pipe(
        Effect.catchCause((cause) =>
          Cause.hasInterruptsOnly(cause)
            ? Effect.void
            : Effect.failCause(cause),
        ),
      ),
    ).finally(() => {
      if (this.fiber === fiber) this.fiber = undefined;
    });
  }

  /** running reports whether replay has started and not yet stopped. */
  running(): boolean {
    return this.fiber !== undefined;
  }

  /** stop interrupts a runner started with replay. */
  async stop(): Promise<void> {
    const fiber = this.fiber;
    this.fiber = undefined;
    if (fiber) await Effect.runPromise(Fiber.interrupt(fiber));
  }
}
