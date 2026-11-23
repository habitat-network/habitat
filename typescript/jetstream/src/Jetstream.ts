import { Duration, Effect, Option, Sink, Stream } from "effect";
import {
  ConnectionError,
  errorMessage,
  FrameDecodeError,
  StreamError,
} from "./errors";
import { decodeFrame, decodeSnapshotPage, type SpaceEvent } from "./events";
import type { LexIndexer } from "./LexIndexer";
import { type EventSink, Runner, type RunnerOptions } from "./Runner";
import { type FetchHandler, fetchTransport, type Transport } from "./transport";

const SUBSCRIBE = "network.habitat.space.subscribe";
const GET_SNAPSHOT = "network.habitat.space.getSnapshot";

export interface JetstreamOptions {
  /** The space host to follow: the org's Habitat instance. */
  readonly service: string;
  /**
   * Only "spaces" exists: one stream across every space the credential can
   * read. Kept so the call site matches @bsky/jetstream's spaces mode.
   */
  readonly mode: "spaces";
  /**
   * The credential the stream and default writes go out with: typically the
   * org's OAuth session's fetchHandler. Called with a path (resolved against
   * its own host) for XRPC calls, and an absolute URL for the stream.
   * Defaults to unauthenticated fetch against `service`.
   */
  readonly fetch?: FetchHandler;
  /** How to open the WebSocket. Defaults to an upgrade through `fetch`. */
  readonly transport?: Transport;
  /** Limit the stream to these spaces (server-side). Default: all. */
  readonly spaces?: readonly string[];
  /** Limit commits to these authors (server-side). Default: all. */
  readonly dids?: readonly string[];
  /** Reconnect backoff: doubles from `base` up to `cap`. */
  readonly reconnect?: {
    readonly base?: Duration.Input;
    readonly cap?: Duration.Input;
  };
}

export interface SubscribeOptions {
  /**
   * Resume position, inclusive: the seq of the first event wanted. 0
   * replays everything; omitted starts at the live tip.
   */
  readonly cursor?: number;
  /** Collection NSIDs or "<prefix>.*" patterns. Default: every collection. */
  readonly collections?: readonly string[];
}

export interface SnapshotOptions {
  /** Start after this seq (exclusive). Default: the start of the log. */
  readonly after?: number;
  /** Collection NSIDs or "<prefix>.*" patterns. Default: every collection. */
  readonly collections?: readonly string[];
  /** Events per page, up to 1000. Default: the server's (100). */
  readonly limit?: number;
}

/** One page of a snapshot, decoded. */
export interface SnapshotPage {
  readonly events: ReadonlyArray<SpaceEvent>;
  /** The last seq the page covered: the next page's `after`. */
  readonly plannedThroughSeq: number;
  /** The end of the snapshot, fixed by its first page. */
  readonly tipSeq: number;
}

/**
 * Jetstream is a client of network.habitat.space.subscribe, Habitat's
 * Jetstream for spaces: every record write and space deletion across the
 * spaces the credential may read, as one sequence with one cursor.
 *
 * The API follows the @bsky/jetstream spaces mode sketched in
 * https://atproto.com/blog/space-sync (`new Jetstream({ service, mode:
 * "spaces" })`, `js.runner(indexer).replay({ cursor })`), so either side can
 * be swapped for the official package as it lands.
 */
export class Jetstream {
  readonly service: URL;
  readonly fetch: FetchHandler;
  private readonly transport: Transport;
  private readonly reconnectBase: Duration.Duration;
  private readonly reconnectCap: Duration.Duration;

  constructor(readonly options: JetstreamOptions) {
    this.service = new URL(options.service);
    const service = this.service;
    this.fetch =
      options.fetch ??
      ((input, init) => globalThis.fetch(new URL(input, service), init));
    this.transport = options.transport ?? fetchTransport(this.fetch);
    this.reconnectBase = Duration.fromInputUnsafe(
      options.reconnect?.base ?? "500 millis",
    );
    this.reconnectCap = Duration.fromInputUnsafe(
      options.reconnect?.cap ?? "30 seconds",
    );
  }

  /** subscribeUrl is the stream's WebSocket URL for a cursor and filter. */
  subscribeUrl(
    cursor: number | undefined,
    collections?: readonly string[],
  ): URL {
    const url = new URL(`/xrpc/${SUBSCRIBE}`, this.service);
    url.protocol = this.service.protocol === "http:" ? "ws:" : "wss:";
    if (cursor !== undefined) url.searchParams.set("cursor", String(cursor));
    for (const space of this.options.spaces ?? []) {
      url.searchParams.append("spaces", space);
    }
    for (const did of this.options.dids ?? []) {
      url.searchParams.append("dids", did);
    }
    for (const c of collections ?? []) {
      url.searchParams.append("collections", c);
    }
    return url;
  }

  /** snapshotUrl is a getSnapshot page's URL. */
  snapshotUrl(
    after: number,
    before: number | undefined,
    options: SnapshotOptions = {},
  ): URL {
    const url = new URL(`/xrpc/${GET_SNAPSHOT}`, this.service);
    url.searchParams.set("afterSeq", String(after));
    if (before !== undefined) url.searchParams.set("beforeSeq", String(before));
    if (options.limit !== undefined) {
      url.searchParams.set("limit", String(options.limit));
    }
    for (const space of this.options.spaces ?? []) {
      url.searchParams.append("spaces", space);
    }
    for (const did of this.options.dids ?? []) {
      url.searchParams.append("dids", did);
    }
    for (const c of options.collections ?? []) {
      url.searchParams.append("collections", c);
    }
    return url;
  }

  /**
   * snapshot pages through the log up to a tip fixed by its first page, the
   * counterpart of Jetstream's replay archive (planSnapshot): a backlog is
   * caught up over plain HTTP, a page at a time, instead of one long
   * WebSocket replay. Each page is emitted as it arrives, and the stream
   * ends when plannedThroughSeq reaches tipSeq. Then `subscribe({ cursor:
   * tipSeq + 1 })` follows live without a gap.
   *
   * A failed page is not retried here: its error ends the stream, and the
   * caller resumes from the last page's plannedThroughSeq.
   */
  snapshot(
    options: SnapshotOptions = {},
  ): Stream.Stream<SnapshotPage, StreamError | FrameDecodeError> {
    const self = this;
    const fetchPage = (
      after: number,
      before: number | undefined,
    ): Effect.Effect<SnapshotPage, StreamError | FrameDecodeError> =>
      Effect.gen(function* () {
        const res = yield* Effect.tryPromise({
          try: () =>
            self.fetch(self.snapshotUrl(after, before, options).toString()),
          catch: (cause) =>
            new StreamError({
              error: "RequestFailed",
              message: `getSnapshot: ${errorMessage(cause)}`,
            }),
        });
        if (!res.ok) {
          const body = yield* Effect.promise(() => res.text().catch(() => ""));
          return yield* new StreamError({
            error: `HTTP ${res.status}`,
            message: `getSnapshot refused: ${body}`,
          });
        }
        const body = yield* Effect.tryPromise({
          try: () => res.json() as Promise<unknown>,
          catch: (cause) =>
            new FrameDecodeError({
              message: `invalid getSnapshot response: ${errorMessage(cause)}`,
            }),
        });
        return yield* decodeSnapshotPage(body);
      });
    return Stream.paginate(
      { after: options.after ?? 0, tip: undefined as number | undefined },
      (state) =>
        fetchPage(state.after, state.tip).pipe(
          Effect.map((page) => [
            [page],
            page.plannedThroughSeq >= page.tipSeq ||
            page.plannedThroughSeq <= state.after
              ? Option.none()
              : Option.some({
                  after: page.plannedThroughSeq,
                  tip: page.tipSeq as number | undefined,
                }),
          ]),
        ),
    );
  }

  /**
   * subscribe streams events from cursor, then live. A dropped connection,
   * or the server ending the stream because the credential expired, is
   * reconnected with backoff from the seq after the last event emitted, so
   * consumers see one unbroken, at-least-once sequence. Only errors that a
   * reconnect can't fix (a refused request, a malformed frame) end it.
   */
  subscribe(
    options: SubscribeOptions = {},
  ): Stream.Stream<SpaceEvent, StreamError | FrameDecodeError> {
    let next = options.cursor;
    let failures = 0;
    const once = (): Stream.Stream<
      SpaceEvent,
      StreamError | FrameDecodeError | ConnectionError
    > =>
      Stream.suspend(() =>
        this.transport(this.subscribeUrl(next, options.collections)),
      ).pipe(
        Stream.mapEffect(decodeFrame),
        Stream.flatMap(
          (frame): Stream.Stream<SpaceEvent, StreamError | ConnectionError> => {
            switch (frame._tag) {
              case "Event":
                next = frame.event.seq + 1;
                failures = 0;
                return Stream.succeed(frame.event);
              case "Error":
                // An expired credential is the normal end of a long stream:
                // reconnect, and the fetch handler refreshes the token.
                if (frame.error === "ExpiredToken") {
                  return Stream.fail(
                    new ConnectionError({ message: "credential expired" }),
                  );
                }
                return Stream.fail(
                  new StreamError({
                    error: frame.error,
                    message: frame.message,
                  }),
                );
              case "Info":
                return Stream.fromEffect(
                  Effect.logInfo("jetstream info", frame),
                ).pipe(Stream.drain);
              case "Unknown":
                return Stream.empty;
            }
          },
        ),
      );
    const withReconnect = (): Stream.Stream<
      SpaceEvent,
      StreamError | FrameDecodeError
    > =>
      once().pipe(
        Stream.catchTag("ConnectionError", (err) => {
          // A refused upgrade other than for auth or availability won't
          // get better by retrying.
          if (
            err.status !== undefined &&
            err.status >= 400 &&
            err.status < 500 &&
            err.status !== 401 &&
            err.status !== 408 &&
            err.status !== 429
          ) {
            return Stream.fail(
              new StreamError({
                error: `HTTP ${err.status}`,
                message: err.message,
              }),
            );
          }
          const delay = Duration.min(
            Duration.times(this.reconnectBase, 2 ** Math.min(failures, 16)),
            this.reconnectCap,
          );
          failures++;
          return Stream.fromEffect(
            Effect.logWarning("jetstream reconnecting", {
              error: err.message,
              delay: Duration.format(delay),
            }).pipe(Effect.andThen(Effect.sleep(delay))),
          ).pipe(Stream.drain, Stream.concat(Stream.suspend(withReconnect)));
        }),
      );
    return withReconnect();
  }

  /** runner runs an indexer's handlers, or a Sink, over this stream; see [Runner]. */
  runner<E>(
    target: LexIndexer<E> | EventSink<E>,
    options?: RunnerOptions,
  ): Runner<E> {
    const sink = Sink.isSink(target) ? target : target.sink();
    const collections =
      options?.collections ??
      (Sink.isSink(target) ? undefined : target.collections());
    return new Runner(this, sink as EventSink<E>, { ...options, collections });
  }
}
