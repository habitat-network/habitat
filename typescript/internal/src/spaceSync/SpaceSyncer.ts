import { SpaceRef, type DidString, type SpaceRefString } from "@atproto/syntax";
import type { com } from "api";
import {
  Clock,
  Context,
  Duration,
  Effect,
  FiberMap,
  Layer,
  Option,
  PubSub,
  Schedule,
  Semaphore,
  Stream,
} from "effect";
import { SpaceSyncConfig } from "./config";
import type { Credentials } from "./Credentials";
import type { InvalidSpaceRefError, SinkError, StoreError } from "./errors";
import type { Identity } from "./Identity";
import { type SpaceCallError, SpaceClient } from "./SpaceClient";
import { type PassKind, recordPassFailure, runSpacePass } from "./spacePass";
import { SyncSink } from "./SyncSink";
import { SyncStore } from "./SyncStore";
import type { SyncEvent } from "./types";
import { toSpaceRef } from "./wire";

const STRENGTH: Record<PassKind, number> = {
  CatchUp: 0,
  Maintenance: 1,
  Full: 2,
};

export class SpaceSyncer extends Context.Service<
  SpaceSyncer,
  {
    readonly watch: (
      space: string,
    ) => Effect.Effect<void, InvalidSpaceRefError | StoreError>;
    readonly unwatch: (
      space: SpaceRefString,
    ) => Effect.Effect<void, StoreError>;
    readonly notifyWrite: (
      input: com.atproto.space.notifyWrite.$InputBody,
    ) => Effect.Effect<void, InvalidSpaceRefError | StoreError>;
    readonly notifySpaceDeleted: (
      space: SpaceRefString,
    ) => Effect.Effect<void, StoreError | SinkError>;
    readonly getBlob: (
      space: SpaceRefString,
      did: DidString,
      cid: string,
    ) => Effect.Effect<Uint8Array, SpaceCallError>;
    /** Committed batches (Reset carries no records), for fan-out such as SSE. */
    readonly events: Stream.Stream<SyncEvent>;
    readonly activeSpaces: Effect.Effect<number>;
    readonly awaitIdle: (space: SpaceRefString) => Effect.Effect<void>;
  }
>()("internal/spaceSync/SpaceSyncer") {
  static readonly layer = Layer.effect(
    SpaceSyncer,
    Effect.gen(function* () {
      const config = yield* SpaceSyncConfig;
      const store = yield* SyncStore;
      const sink = yield* SyncSink;
      const client = yield* SpaceClient;
      const services = yield* Effect.context<
        SpaceClient | Identity | Credentials | SyncStore | SyncSink
      >();
      const permits = yield* Semaphore.make(config.maxActiveSpaces);
      const fibers = yield* FiberMap.make<string>();
      const events = yield* PubSub.unbounded<SyncEvent>();

      // Mailbox: at most one pending trigger per space (the strongest). `running`
      // maps a space to the token of the fiber draining it; the token stops a
      // finishing fiber from clearing the entry of its replacement.
      const pending = new Map<string, PassKind>();
      const running = new Map<string, number>();
      let nextToken = 0;

      const deleteSpaceData = (space: SpaceRefString) =>
        Effect.gen(function* () {
          pending.delete(space);
          yield* sink.apply({ _tag: "SpaceDeleted", space });
          yield* store.removeSpace(space);
          yield* PubSub.publish(events, { _tag: "SpaceDeleted", space });
        }).pipe(Effect.uninterruptible);

      const drain = (
        space: SpaceRefString,
        token: number,
      ): Effect.Effect<void> =>
        Effect.gen(function* () {
          while (true) {
            // Take the next trigger or release `running` in the same synchronous
            // step, so an enqueue can never observe a fiber that has stopped draining.
            const kind = yield* Effect.sync(() => {
              const next = pending.get(space);
              if (next === undefined) {
                if (running.get(space) === token) running.delete(space);
                return undefined;
              }
              pending.delete(space);
              return next;
            });
            if (kind === undefined) return;
            yield* runSpacePass(space, kind).pipe(
              Effect.flatMap((evts) => PubSub.publishAll(events, evts)),
              Effect.catchTag("CredentialError", (error) =>
                error.reason === "SpaceDeleted"
                  ? deleteSpaceData(space)
                  : recordPassFailure(space, error),
              ),
              Effect.catch((error) => recordPassFailure(space, error)),
              Effect.provideContext(services),
              Effect.annotateLogs({ space }),
            );
          }
        }).pipe(
          permits.withPermits(1),
          Effect.ensuring(
            Effect.sync(
              () =>
                void (running.get(space) === token && running.delete(space)),
            ),
          ),
        );

      const enqueue = (space: SpaceRefString, kind: PassKind) =>
        Effect.gen(function* () {
          const token = yield* Effect.sync(() => {
            const prev = pending.get(space);
            if (prev === undefined || STRENGTH[kind] > STRENGTH[prev])
              pending.set(space, kind);
            if (running.has(space)) return undefined;
            const t = ++nextToken;
            running.set(space, t);
            return t;
          });
          if (token !== undefined)
            yield* FiberMap.run(fibers, space, { onlyIfMissing: false })(
              drain(space, token),
            );
        });

      const stop = (space: SpaceRefString) =>
        Effect.gen(function* () {
          pending.delete(space);
          yield* FiberMap.remove(fibers, space);
          running.delete(space);
        });

      // Single scheduler: idle spaces live only in the store. Enqueued spaces get
      // a lease (nextDueAt = now + backoffCap) so later ticks skip them while they
      // wait for a permit; the pass writes the real nextDueAt.
      const tick = Effect.gen(function* () {
        const now = yield* Clock.currentTimeMillis;
        const due = yield* store.dueSpaces(now, config.schedulerPageSize);
        const fullIntervalMs = Duration.toMillis(config.fullPassInterval);
        for (const state of due) {
          if (running.has(state.space)) continue;
          yield* store.putSpace({
            ...state,
            nextDueAt: now + Duration.toMillis(config.backoffCap),
          });
          yield* enqueue(
            state.space,
            (state.lastFullPassAt ?? 0) + fullIntervalMs <= now
              ? "Full"
              : "Maintenance",
          );
        }
      }).pipe(
        Effect.catch((error) =>
          Effect.logWarning("scheduler tick failed", error),
        ),
      );
      yield* tick.pipe(
        Effect.repeat(Schedule.spaced(config.schedulerInterval)),
        Effect.forkScoped,
      );

      return SpaceSyncer.of({
        watch: Effect.fn("SpaceSyncer.watch")(function* (untrusted: string) {
          const space = yield* toSpaceRef(untrusted);
          if (Option.isNone(yield* store.getSpace(space))) {
            const now = yield* Clock.currentTimeMillis;
            yield* store.putSpace({
              space,
              authority: SpaceRef.parse(space).spaceDid,
              nextDueAt: now + Duration.toMillis(config.backoffCap),
              failures: 0,
            });
          }
          yield* enqueue(space, "Full");
        }),
        unwatch: Effect.fn("SpaceSyncer.unwatch")(function* (
          space: SpaceRefString,
        ) {
          yield* stop(space);
          yield* store.removeSpace(space);
        }),
        notifyWrite: Effect.fn("SpaceSyncer.notifyWrite")(function* (
          input: com.atproto.space.notifyWrite.$InputBody,
        ) {
          const space = yield* toSpaceRef(input.space);
          const state = yield* store.getSpace(space);
          if (Option.isNone(state)) return;
          // Never trusted as a checkpoint: it only decides whether a catch-up pass is worth running.
          if (
            input.spaceRev &&
            state.value.spaceRev &&
            input.spaceRev <= state.value.spaceRev
          )
            return;
          yield* enqueue(space, "CatchUp");
        }),
        notifySpaceDeleted: Effect.fn("SpaceSyncer.notifySpaceDeleted")(
          function* (space: SpaceRefString) {
            if (Option.isNone(yield* store.getSpace(space))) return;
            yield* stop(space);
            yield* deleteSpaceData(space);
          },
        ),
        getBlob: (space, did, cid) => client.getBlob(space, did, cid),
        events: Stream.fromPubSub(events),
        activeSpaces: FiberMap.size(fibers),
        awaitIdle: (space) =>
          Effect.sync(() => running.has(space) || pending.has(space)).pipe(
            Effect.repeat({
              schedule: Schedule.spaced("5 millis"),
              while: (busy: boolean) => busy,
            }),
            Effect.asVoid,
          ),
      });
    }),
  );
}
