import { lexStringify } from "@atproto/lex-json";
import {
  Cause,
  Duration,
  Effect,
  Fiber,
  Option,
  PubSub,
  Schedule,
  Sink,
  Schema,
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
import type { Jetstream } from "./Jetstream";
import type { FetchHandler } from "./transport";

/**
 * Where the runner keeps its cursor: the seq to resume from, which is one
 * past the last event every handler finished. One value for the whole
 * stream, however many spaces and repos it spans.
 */
export interface CursorStore<E = unknown> {
  readonly get: Effect.Effect<number | undefined, E>;
  readonly set: (cursor: number) => Effect.Effect<void, E>;
}

export interface RunnerOptions {
  /** Persists the cursor. Without one the runner keeps it in memory. */
  readonly cursor?: CursorStore;
  /** How long a write waits for its event to be indexed. Default 15s. */
  readonly writeTimeout?: Duration.Input;
  /** How many recently indexed events are replayed to a late `awaitIndexed`. */
  readonly recentLimit?: number;
  /**
   * The collections to ask the server for. Default: all of them. (`js.runner`
   * fills it in from a LexIndexer.)
   */
  readonly collections?: ReadonlyArray<string>;
  /**
   * Hands the sink events in groups: up to `size` of them, or whatever has
   * arrived `window` after the first. A sink can then write a group at once
   * (one transaction); the cursor advances once per group. Default: one
   * event at a time.
   */
  readonly batch?: { readonly size: number; readonly window?: Duration.Input };
}

/**
 * What a Runner feeds events to: a LexIndexer, or any Effect Sink over the
 * stream's events. Each run of the sink gets one group of events as its
 * whole input (see RunnerOptions.batch), so it ends when they're consumed.
 * It may be run again over the same events after a failure, so it must be
 * idempotent.
 */
export type EventSink<E = unknown> = Sink.Sink<void, SpaceEvent, never, E>;

export interface ReplayOptions {
  /**
   * The seq to start from, inclusive. Defaults to the cursor store's, else
   * 0: a fresh runner replays the (compacted) log and converges on the
   * current state before following it live.
   */
  readonly cursor?: number;
  /**
   * Catch up over getSnapshot, a page of HTTP at a time, before opening the
   * WebSocket at the snapshot's tip. Only worthwhile for a large backlog,
   * such as a fresh runner or one that was off for long; a runner already
   * near the head may as well subscribe directly. Default: false.
   */
  readonly snapshot?: boolean | { readonly limit?: number };
  /**
   * Called once the snapshot pages (if any) are applied, as the live socket
   * is about to open from the saved position: everything up to the
   * snapshot's tip is indexed. Without `snapshot` there are no pages, so it
   * is called at once and the socket does the catch-up.
   */
  readonly onCaughtUp?: () => void;
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

const WriteResultBody = Schema.Struct({
  uri: Schema.String,
  cid: Schema.String,
});

const XrpcErrorBody = Schema.Struct({
  error: Schema.optional(Schema.String),
  message: Schema.optional(Schema.String),
});

const HANDLER_RETRY = Schedule.exponential("200 millis").pipe(
  Schedule.modifyDelay((m) =>
    Effect.succeed(Duration.min(m.duration, Duration.minutes(1))),
  ),
);

/**
 * Runner runs a LexIndexer's handlers, or an Effect Sink, over a Jetstream,
 * one event (or group of events, see RunnerOptions.batch) at a time, and advances the cursor only after an event's handlers all succeed:
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
  // Every event the sink has finished, for awaitIndexed. It replays the last
  // `recentLimit` to a late subscriber, so a write whose event was indexed
  // before its response arrived still finds it.
  private readonly indexedEvents: PubSub.PubSub<CommitEvent>;
  private readonly writeTimeout: Duration.Duration;
  private memoryCursor: number | undefined;
  private fiber: Fiber.Fiber<void, unknown> | undefined;

  constructor(
    readonly jetstream: Jetstream,
    private readonly sink: EventSink<E>,
    readonly options: RunnerOptions = {},
  ) {
    this.indexedEvents = Effect.runSync(
      PubSub.unbounded<CommitEvent>({ replay: options.recentLimit ?? 1_000 }),
    );
    this.writeTimeout = Duration.fromInputUnsafe(
      options.writeTimeout ?? "15 seconds",
    );
  }

  /** The cursor the runner would resume from now. */
  cursor(): Effect.Effect<number | undefined, CursorError> {
    const store = this.options.cursor;
    if (!store) return Effect.succeed(this.memoryCursor);
    return store.get.pipe(
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
    return store.set(cursor).pipe(
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
  readonly run = Effect.fn("Runner.run")(
    (
      options: ReplayOptions = {},
    ): Effect.Effect<void, StreamError | FrameDecodeError | CursorError> => {
      const { batch } = this.options;
      const collections = this.options.collections
        ? [...this.options.collections]
        : undefined;
      const size = batch && batch.size > 1 ? batch.size : 1;
      // Applies events a group at a time, whether they came from a snapshot
      // page or the socket.
      const applyGroups = (events: ReadonlyArray<SpaceEvent>) =>
        Stream.fromArray(events).pipe(
          Stream.rechunk(size),
          Stream.runForEachArray((group) => this.applyAll(group)),
        );
      return Effect.gen({ self: this }, function* () {
        let cursor = options.cursor ?? (yield* this.cursor()) ?? 0;
        if (options.snapshot) {
          const limit =
            typeof options.snapshot === "object"
              ? options.snapshot.limit
              : undefined;
          yield* this.jetstream
            // afterSeq is exclusive and never negative: seqs start at 1, so
            // a fresh runner (cursor 0) asks for everything after 0.
            .snapshot({ after: Math.max(cursor - 1, 0), collections, limit })
            .pipe(
              Stream.runForEach((page) =>
                Effect.gen({ self: this }, function* () {
                  yield* applyGroups(page.events);
                  // The page may end past its last event, over events the
                  // reader can't see: don't scan them again.
                  cursor = page.plannedThroughSeq + 1;
                  yield* this.saveCursor(cursor);
                }),
              ),
            );
        }
        options.onCaughtUp?.();
        yield* this.jetstream.subscribe({ cursor, collections }).pipe(
          Stream.groupedWithin(size, batch?.window ?? "10 millis"),
          Stream.runForEach((group) => this.applyAll(group)),
        );
      });
    },
  );

  /** apply indexes one event and settles the writes waiting on it. */
  apply(event: SpaceEvent): Effect.Effect<void, CursorError> {
    return this.applyAll([event]);
  }

  /**
   * applyAll runs the sink over events (a group, in stream order), retrying
   * with backoff until it succeeds, then advances the cursor past them and
   * settles the writes waiting on them.
   */
  readonly applyAll = Effect.fn("Runner.applyAll")(
    (events: ReadonlyArray<SpaceEvent>): Effect.Effect<void, CursorError> => {
      const first = events[0];
      const last = events[events.length - 1];
      if (!first || !last) return Effect.void;
      return Effect.gen({ self: this }, function* () {
        yield* Stream.fromArray(events).pipe(
          Stream.run(this.sink),
          Effect.mapError(
            (cause) =>
              new HandlerError({
                uri: first.kind === "spaceDeleted" ? first.space : first.uri,
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
        yield* this.saveCursor(last.seq + 1);
        for (const event of events) {
          if (event.kind !== "spaceDeleted") {
            yield* PubSub.publish(this.indexedEvents, event);
          }
        }
      });
    },
  );

  /**
   * awaitIndexed waits until the record at uri has been indexed with cid
   * (undefined: deleted), or fails with WriteNotObserved after the timeout.
   */
  awaitIndexed(
    uri: string,
    cid: string | undefined,
    timeout: Duration.Input = this.writeTimeout,
  ): Effect.Effect<void, WriteNotObserved> {
    return Effect.scoped(
      PubSub.subscribe(this.indexedEvents).pipe(
        Effect.flatMap((subscription) =>
          Stream.fromSubscription(subscription).pipe(
            Stream.filter(
              (e) =>
                e.uri === uri && (e.kind === "put" ? e.cid : undefined) === cid,
            ),
            Stream.runHead,
          ),
        ),
        Effect.timeoutOrElse({
          duration: timeout,
          orElse: () => Effect.fail(new WriteNotObserved({ uri, cid })),
        }),
        Effect.asVoid,
      ),
    );
  }

  // xrpc calls an XRPC method with fetch, and decodes a successful body with
  // schema. A refusal is a WriteError carrying the server's error name.
  private readonly xrpc = Effect.fn("Runner.xrpc")(function* <A, I>(
    nsid: string,
    method: "GET" | "POST",
    params: Record<string, unknown>,
    fetch: FetchHandler,
    schema: Schema.Codec<A, I>,
  ): Effect.fn.Return<A, WriteError> {
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
    const fail = (status: number, message: string, error?: string) =>
      new WriteError({
        nsid,
        status,
        message,
        ...(error ? { error } : {}),
      });
    const res = yield* Effect.tryPromise({
      try: () => fetch(path, init),
      catch: (cause) => fail(0, errorMessage(cause)),
    });
    const text = yield* Effect.promise(() => res.text().catch(() => ""));
    const body: unknown = yield* Effect.try(() =>
      text ? JSON.parse(text) : undefined,
    ).pipe(Effect.orElseSucceed(() => undefined));
    if (!res.ok) {
      const refusal = yield* Schema.decodeUnknownEffect(XrpcErrorBody)(
        body,
      ).pipe(Effect.orElseSucceed(() => ({}) as typeof XrpcErrorBody.Type));
      return yield* fail(res.status, refusal.message ?? text, refusal.error);
    }
    return yield* Schema.decodeUnknownEffect(schema)(body).pipe(
      Effect.mapError((cause) =>
        fail(res.status, `unexpected response: ${errorMessage(cause)}`),
      ),
    );
  });

  private write(
    nsid: "com.atproto.space.putRecord" | "com.atproto.space.createRecord",
    input: PutRecordInput | CreateRecordInput,
    options: WriteOptions,
  ): Effect.Effect<WriteResult, WriteError | WriteNotObserved> {
    return this.xrpc(
      nsid,
      "POST",
      { ...input },
      options.fetch ?? this.jetstream.fetch,
      WriteResultBody,
    ).pipe(
      Effect.tap(({ uri, cid }) =>
        this.awaitIndexed(uri, cid, options.timeout),
      ),
    );
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
    return this.write("com.atproto.space.putRecord", input, options);
  }

  /** createRecord is putRecord for com.atproto.space.createRecord. */
  createRecord(
    input: CreateRecordInput,
    options: WriteOptions = {},
  ): Effect.Effect<WriteResult, WriteError | WriteNotObserved> {
    return this.write("com.atproto.space.createRecord", input, options);
  }

  /**
   * deleteRecord deletes a record with com.atproto.space.deleteRecord, then
   * waits until the runner has indexed the delete. Deleting a record that
   * doesn't exist produces no event, so it's looked up first, and returns
   * at once when it's already gone.
   */
  readonly deleteRecord = Effect.fn("Runner.deleteRecord")(function* (
    this: Runner<E>,
    input: RecordTarget,
    options: WriteOptions = {},
  ): Effect.fn.Return<void, WriteError | WriteNotObserved> {
    const fetch = options.fetch ?? this.jetstream.fetch;
    const found = yield* this.xrpc(
      "com.atproto.space.getRecord",
      "GET",
      { ...input },
      fetch,
      Schema.Struct({ uri: Schema.String }),
    ).pipe(
      Effect.map(Option.some),
      Effect.catchIf(
        (e) => e.status === 404 || e.error === "RecordNotFound",
        () => Effect.succeed(Option.none()),
      ),
    );
    yield* this.xrpc(
      "com.atproto.space.deleteRecord",
      "POST",
      { ...input },
      fetch,
      Schema.Unknown,
    );
    if (Option.isSome(found)) {
      yield* this.awaitIndexed(found.value.uri, undefined, options.timeout);
    }
  });

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
