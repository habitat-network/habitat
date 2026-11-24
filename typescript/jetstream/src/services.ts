import { Context, Effect, Layer, Sink } from "effect";
import { JetstreamClient, type JetstreamOptions } from "./Jetstream";
import type { LexIndexer } from "./LexIndexer";
import { type EventSink, RunnerClient, type RunnerOptions } from "./Runner";

/**
 * The Effect face of the SDK: `Jetstream` and `Runner` as services, built
 * with a Layer. Effect programs depend on these directly; everyone else uses
 * the promise-based wrappers at the package root (see ./api.ts), which are
 * built from the same pieces.
 *
 * ```ts
 * const program = Effect.gen(function* () {
 *   const runner = yield* Runner;
 *   yield* runner.run();
 * });
 * program.pipe(
 *   Effect.provide(Runner.layer({ target: indexer })),
 *   Effect.provide(Jetstream.layer({ service, mode: "spaces", fetch })),
 * );
 * ```
 */
export class Jetstream extends Context.Service<Jetstream, JetstreamClient>()(
  "@habitat-network/jetstream/Jetstream",
) {
  /** layer provides a Jetstream for the host and credential in options. */
  static layer(options: JetstreamOptions): Layer.Layer<Jetstream> {
    return Layer.sync(Jetstream, () => new JetstreamClient(options));
  }
}

export interface RunnerLayerOptions<E> extends RunnerOptions {
  /** What the runner feeds events to: an Effect LexIndexer, or any Sink. */
  readonly target: LexIndexer<E> | EventSink<E>;
}

/** sinkOf adapts a runner target to the Sink and collections it needs. */
export function sinkOf<E>(
  target: LexIndexer<E> | EventSink<E>,
  options: RunnerOptions = {},
): { sink: EventSink<E>; options: RunnerOptions } {
  if (Sink.isSink(target)) {
    return { sink: target as EventSink<E>, options };
  }
  const indexer = target as LexIndexer<E>;
  return {
    sink: indexer.sink() as EventSink<E>,
    options: {
      ...options,
      collections: options.collections ?? indexer.collections(),
    },
  };
}

export class Runner extends Context.Service<Runner, RunnerClient>()(
  "@habitat-network/jetstream/Runner",
) {
  /** layer provides a Runner over the Jetstream in context. */
  static layer<E>({
    target,
    ...options
  }: RunnerLayerOptions<E>): Layer.Layer<Runner, never, Jetstream> {
    return Layer.effect(
      Runner,
      Effect.gen(function* () {
        const jetstream = yield* Jetstream;
        const adapted = sinkOf(target, options);
        return new RunnerClient(jetstream, adapted.sink, adapted.options);
      }),
    );
  }
}
