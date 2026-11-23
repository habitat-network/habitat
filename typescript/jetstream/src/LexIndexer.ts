import { Effect, Sink } from "effect";
import type {
  DelEvent,
  PutEvent,
  SpaceDeletedEvent,
  SpaceEvent,
} from "./events";
import { type HandlerResult, toEffect } from "./handler";

/**
 * A collection to index: its NSID, a "<prefix>.*" pattern, or a generated
 * @atproto/lex namespace (e.g. `network.habitat.drop.file`), whose
 * `$safeParse` then validates records and types the handlers. Records that
 * fail validation are skipped, as they could never be indexed.
 */
export type CollectionRef<R> =
  | string
  | {
      readonly $nsid: string;
      readonly $safeParse?: (
        value: unknown,
      ) => { success: true; value: R } | { success: false };
    };

export interface CommitHandlers<R, E> {
  /** A record was created or updated. */
  readonly put?: (event: PutEvent<R>) => HandlerResult<E>;
  /** A record was deleted. */
  readonly del?: (event: DelEvent) => HandlerResult<E>;
}

export interface LexIndexerOptions {
  /** Only "spaces"; see [JetstreamOptions.mode]. */
  readonly mode: "spaces";
}

interface Registration {
  readonly collection: string;
  readonly matches: (collection: string) => boolean;
  readonly parse?: (
    value: unknown,
  ) => { success: true; value: unknown } | { success: false };
  readonly put?: (event: PutEvent<unknown>) => HandlerResult<unknown>;
  readonly del?: (event: DelEvent) => HandlerResult<unknown>;
}

/**
 * LexIndexer routes stream events to per-collection handlers, like
 * @bsky/jetstream's LexIndexer: `.commit(collection, { put, del })` for
 * record writes and deletes, `.spaceDeleted(fn)` for deleted spaces.
 * Delivery is at-least-once, so handlers must be idempotent: key them on
 * the event's uri.
 */
export class LexIndexer<E = unknown> {
  private readonly registrations: Registration[] = [];
  private onSpaceDeleted: Array<
    (event: SpaceDeletedEvent) => HandlerResult<E>
  > = [];

  constructor(readonly options: LexIndexerOptions = { mode: "spaces" }) {}

  commit<R = Record<string, unknown>>(
    collection: CollectionRef<R>,
    handlers: CommitHandlers<R, E>,
  ): this {
    const nsid = typeof collection === "string" ? collection : collection.$nsid;
    const prefix = nsid.endsWith(".*") ? nsid.slice(0, -1) : undefined;
    this.registrations.push({
      collection: nsid,
      matches: prefix ? (c) => c.startsWith(prefix) : (c) => c === nsid,
      parse: typeof collection === "string" ? undefined : collection.$safeParse,
      put: handlers.put as Registration["put"],
      del: handlers.del,
    });
    return this;
  }

  spaceDeleted(handler: (event: SpaceDeletedEvent) => HandlerResult<E>): this {
    this.onSpaceDeleted.push(handler);
    return this;
  }

  /**
   * The collections to ask the server for, or undefined for all of them
   * when no commit handler is registered.
   */
  collections(): string[] | undefined {
    if (this.registrations.length === 0) return undefined;
    return [...new Set(this.registrations.map((r) => r.collection))];
  }

  /**
   * sink is this indexer as a Sink over the stream's events: each event goes
   * through [handle], in order. It's how a Runner runs an indexer.
   */
  sink(): Sink.Sink<void, SpaceEvent, never, unknown> {
    return Sink.forEach((event: SpaceEvent) => this.handle(event));
  }

  /** handle runs every handler registered for event, in order. */
  handle(event: SpaceEvent): Effect.Effect<void, unknown> {
    const self = this;
    return Effect.gen(function* () {
      if (event.kind === "spaceDeleted") {
        for (const fn of self.onSpaceDeleted) yield* toEffect(() => fn(event));
        return;
      }
      for (const r of self.registrations) {
        if (!r.matches(event.collection)) continue;
        if (event.kind === "del") {
          if (r.del) yield* toEffect(() => r.del!(event));
          continue;
        }
        if (!r.put) continue;
        let record: unknown = event.record;
        if (r.parse) {
          const parsed = r.parse(event.record);
          if (!parsed.success) {
            yield* Effect.logWarning("jetstream: skipping invalid record", {
              uri: event.uri,
            });
            continue;
          }
          record = parsed.value;
        }
        const put = r.put;
        yield* toEffect(() => put({ ...event, record }));
      }
    });
  }
}
