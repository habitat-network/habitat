import { Cause, Effect, Fiber } from "effect";
import { JetstreamClient, type JetstreamOptions } from "./Jetstream";
import {
  LexIndexer as EffectLexIndexer,
  type CollectionRef,
  type LexIndexerOptions,
} from "./LexIndexer";
import {
  type CreateRecordInput,
  type CursorStore,
  type EventSink,
  type PutRecordInput,
  type RecordTarget,
  type ReplayOptions,
  type RunnerClient,
  type RunnerOptions,
  type WriteOptions,
  type WriteResult,
} from "./Runner";
import type { DelEvent, PutEvent, SpaceDeletedEvent } from "./events";
import { sinkOf } from "./services";
import { RunnerClient as Client } from "./Runner";
import type { Duration } from "effect";

/**
 * The promise-based face of the SDK, shaped after @bsky/jetstream's spaces
 * mode: `new Jetstream({ service, mode: "spaces" })`, `new LexIndexer()`,
 * `js.runner(indexer).replay()`. Handlers and cursor stores return promises
 * (or nothing); writes return promises that reject with the SDK's tagged
 * errors. Effect programs use the services in ./services.ts instead, and
 * `runner.effect` reaches the Effect runner behind a wrapper.
 */

/** What a promise-based handler returns: a promise, or nothing. */
export type HandlerResult = PromiseLike<void> | void;

export interface PromiseCommitHandlers<R> {
  readonly put?: (event: PutEvent<R>) => HandlerResult;
  readonly del?: (event: DelEvent) => HandlerResult;
}

const attempt = (f: () => HandlerResult) =>
  Effect.tryPromise(async () => {
    await f();
  });

/** LexIndexer routes events to promise handlers, like @bsky/jetstream's. */
export class LexIndexer {
  /** The Effect indexer behind this one. */
  readonly effect: EffectLexIndexer<unknown>;

  constructor(options: LexIndexerOptions = { mode: "spaces" }) {
    this.effect = new EffectLexIndexer(options);
  }

  commit<R = Record<string, unknown>>(
    collection: CollectionRef<R>,
    handlers: PromiseCommitHandlers<R>,
  ): this {
    const { put, del } = handlers;
    this.effect.commit(collection, {
      ...(put && { put: (e: PutEvent<R>) => attempt(() => put(e)) }),
      ...(del && { del: (e: DelEvent) => attempt(() => del(e)) }),
    });
    return this;
  }

  spaceDeleted(handler: (event: SpaceDeletedEvent) => HandlerResult): this {
    this.effect.spaceDeleted((e) => attempt(() => handler(e)));
    return this;
  }
}

/** Where a promise-based runner keeps its cursor. */
export interface PromiseCursorStore {
  get(): PromiseLike<number | undefined> | number | undefined;
  set(cursor: number): PromiseLike<void> | void;
}

export interface PromiseRunnerOptions extends Omit<RunnerOptions, "cursor"> {
  /** Persists the cursor. Without one the runner keeps it in memory. */
  readonly cursor?: PromiseCursorStore;
}

export class Jetstream {
  /** The Effect client behind this one. */
  readonly effect: JetstreamClient;

  constructor(options: JetstreamOptions) {
    this.effect = new JetstreamClient(options);
  }

  /** runner runs an indexer's handlers, or an Effect Sink, over the stream. */
  runner<E = unknown>(
    target: LexIndexer | EffectLexIndexer<E> | EventSink<E>,
    options: PromiseRunnerOptions = {},
  ): Runner {
    const effectTarget = target instanceof LexIndexer ? target.effect : target;
    const { cursor, ...rest } = options;
    const store: CursorStore | undefined = cursor && {
      get: Effect.tryPromise(async () => cursor.get()),
      set: (c) => Effect.tryPromise(async () => cursor.set(c)),
    };
    const adapted = sinkOf(effectTarget as EffectLexIndexer<E> | EventSink<E>, {
      ...rest,
      ...(store && { cursor: store }),
    });
    return new Runner(new Client(this.effect, adapted.sink, adapted.options));
  }
}

export class Runner {
  private fiber: Fiber.Fiber<void, unknown> | undefined;

  constructor(
    /** The Effect runner behind this one. */
    readonly effect: RunnerClient,
  ) {}

  /** The cursor the runner would resume from now. */
  cursor(): Promise<number | undefined> {
    return Effect.runPromise(this.effect.cursor());
  }

  /**
   * replay starts the runner in the background, like @bsky/jetstream's
   * `js.runner(indexer).replay({ cursor })`. The returned promise settles
   * when the runner stops: with stop(), or on an error it can't recover
   * from.
   */
  replay(options: ReplayOptions = {}): Promise<void> {
    if (this.fiber) throw new Error("runner already started");
    const fiber = Effect.runFork(this.effect.run(options));
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

  /**
   * putRecord writes a record, then resolves once it's indexed. Rejects with
   * WriteError (nothing written) or WriteNotObserved (written, not indexed
   * in time).
   */
  putRecord(
    input: PutRecordInput,
    options?: WriteOptions,
  ): Promise<WriteResult> {
    return Effect.runPromise(this.effect.putRecord(input, options));
  }

  createRecord(
    input: CreateRecordInput,
    options?: WriteOptions,
  ): Promise<WriteResult> {
    return Effect.runPromise(this.effect.createRecord(input, options));
  }

  deleteRecord(input: RecordTarget, options?: WriteOptions): Promise<void> {
    return Effect.runPromise(this.effect.deleteRecord(input, options));
  }

  /** awaitIndexed resolves once uri is indexed at cid (undefined: deleted). */
  awaitIndexed(
    uri: string,
    cid: string | undefined,
    timeout?: Duration.Input,
  ): Promise<void> {
    return Effect.runPromise(this.effect.awaitIndexed(uri, cid, timeout));
  }
}
