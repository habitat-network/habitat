import type { DidString, SpaceRef, SpaceRefString } from "@atproto/syntax";
import {
  Cause,
  Clock,
  Context,
  Deferred,
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
import type { SinkError, StoreError } from "./errors";
import type { Identity } from "./Identity";
import { type SpaceCallError, SpaceClient } from "./SpaceClient";
import { RepoBackoff, recordPassFailure, runSpacePass } from "./spacePass";
import { SyncSink } from "./SyncSink";
import { SyncStore } from "./SyncStore";
import type { SyncEvent } from "./types";
import { parseSpaceRef } from "./wire";

export class SpaceSyncer extends Context.Service<
  SpaceSyncer,
  {
    readonly watch: (space: SpaceRef) => Effect.Effect<void, StoreError>;
    readonly unwatch: (space: SpaceRef) => Effect.Effect<void, StoreError>;
    /** A verified notifyWrite; `spaceRev` only decides whether a catch-up pass is worth running. */
    readonly notifyWrite: (
      space: SpaceRef,
      spaceRev: string | undefined,
    ) => Effect.Effect<void, StoreError>;
    readonly notifySpaceDeleted: (
      space: SpaceRef,
    ) => Effect.Effect<void, StoreError | SinkError>;
    readonly getBlob: (
      space: SpaceRef,
      did: DidString,
      cid: string,
    ) => Effect.Effect<Uint8Array, SpaceCallError>;
    /**
     * Committed batches (Reset carries no records), for fan-out such as SSE. A
     * subscriber more than `eventBufferSize` events behind loses the oldest.
     */
    readonly events: Stream.Stream<SyncEvent>;
    readonly activeSpaces: Effect.Effect<number>;
    /** Resolves once the space has no pass running or queued. */
    readonly awaitIdle: (space: SpaceRef) => Effect.Effect<void>;
  }
>()("internal/spaceSync/SpaceSyncer") {
  static readonly layer = Layer.effect(
    SpaceSyncer,
    Effect.gen(function* () {
      const config = yield* SpaceSyncConfig;
      const store = yield* SyncStore;
      const sink = yield* SyncSink;
      const client = yield* SpaceClient;
      const backoff = yield* RepoBackoff;
      const services = yield* Effect.context<
        | SpaceClient
        | Identity
        | Credentials
        | SyncStore
        | SyncSink
        | RepoBackoff
      >();
      const permits = yield* Semaphore.make(config.maxActiveSpaces);
      const fibers = yield* FiberMap.make<SpaceRefString>();
      const events = yield* PubSub.sliding<SyncEvent>(config.eventBufferSize);
      const leaseMs = Duration.toMillis(config.backoffCap);

      // Mailbox: at most one pending trigger per space, `full` OR-merged. `running`
      // maps a space to the token of the fiber draining it; the token stops a
      // finishing fiber from clearing the entry of its replacement. `stopping`
      // counts removals in progress, during which nothing new may start.
      const pending = new Map<
        SpaceRefString,
        { readonly ref: SpaceRef; readonly full: boolean }
      >();
      const running = new Map<SpaceRefString, number>();
      const stopping = new Map<SpaceRefString, number>();
      const idle = new Map<SpaceRefString, Deferred.Deferred<void>>();
      let nextToken = 0;

      const settleIdle = (id: SpaceRefString) => {
        const waiter = idle.get(id);
        if (!waiter || running.has(id) || pending.has(id)) return;
        idle.delete(id);
        Deferred.doneUnsafe(waiter, Effect.void);
      };

      const release = (id: SpaceRefString, token: number) => {
        if (running.get(id) === token) running.delete(id);
        settleIdle(id);
      };

      const deleteSpaceData = (ref: SpaceRef) => {
        const space = ref.toString();
        return Effect.gen(function* () {
          pending.delete(space);
          yield* sink.apply({ _tag: "SpaceDeleted", space });
          yield* store.removeSpace(space);
          yield* backoff.clearSpace(space);
          yield* PubSub.publish(events, { _tag: "SpaceDeleted", space });
        }).pipe(Effect.uninterruptible);
      };

      const pass = (ref: SpaceRef, full: boolean) => {
        const space = ref.toString();
        return runSpacePass(ref, full).pipe(
          Effect.flatMap((evts) => PubSub.publishAll(events, evts)),
          Effect.catchTag("CredentialError", (error) =>
            error.reason === "SpaceDeleted"
              ? deleteSpaceData(ref)
              : recordPassFailure(space, error),
          ),
          Effect.catch((error) => recordPassFailure(space, error)),
          // Defects would otherwise vanish into the FiberMap unreported.
          Effect.catchCause((cause) =>
            Cause.hasInterruptsOnly(cause)
              ? Effect.failCause(cause)
              : Effect.logError("space pass died", Cause.pretty(cause)),
          ),
          // One permit per pass, so a busy space can't hold a slot indefinitely.
          permits.withPermits(1),
          Effect.provideContext(services),
          Effect.annotateLogs({ space }),
        );
      };

      const drain = (id: SpaceRefString, token: number): Effect.Effect<void> =>
        Effect.gen(function* () {
          while (true) {
            // Take the next trigger or release `running` in the same synchronous
            // step, so an enqueue can never observe a fiber that has stopped draining.
            const next = yield* Effect.sync(() => {
              const trigger = pending.get(id);
              if (trigger === undefined) {
                release(id, token);
                return undefined;
              }
              pending.delete(id);
              return trigger;
            });
            if (next === undefined) return;
            yield* pass(next.ref, next.full);
          }
        }).pipe(Effect.ensuring(Effect.sync(() => release(id, token))));

      const enqueue = (ref: SpaceRef, full: boolean) =>
        Effect.gen(function* () {
          const id = ref.toString();
          const token = yield* Effect.sync(() => {
            if (stopping.has(id)) return undefined;
            pending.set(id, { ref, full: full || !!pending.get(id)?.full });
            if (running.has(id)) return undefined;
            const t = ++nextToken;
            running.set(id, t);
            return t;
          });
          if (token !== undefined)
            yield* FiberMap.run(fibers, id, { onlyIfMissing: false })(
              drain(id, token),
            );
        });

      /**
       * Stop the space's fiber and run `remove` with new passes held off until
       * it finishes, so a notification can't start a pass that writes the space
       * back while it is being removed.
       */
      const removing = <E>(ref: SpaceRef, remove: Effect.Effect<void, E>) => {
        const id = ref.toString();
        return Effect.acquireUseRelease(
          Effect.sync(() => {
            stopping.set(id, (stopping.get(id) ?? 0) + 1);
            pending.delete(id);
          }),
          () =>
            FiberMap.remove(fibers, id).pipe(
              Effect.andThen(
                Effect.sync(() => {
                  running.delete(id);
                  settleIdle(id);
                }),
              ),
              Effect.andThen(remove),
            ),
          () =>
            Effect.sync(() => {
              const count = (stopping.get(id) ?? 1) - 1;
              if (count === 0) stopping.delete(id);
              else stopping.set(id, count);
            }),
        );
      };

      // Single scheduler: idle spaces live only in the store. A due space is
      // leased (compare-and-set nextDueAt = now + backoffCap) so later ticks skip
      // it while it waits for a permit; the pass writes the real nextDueAt.
      const tick = Effect.gen(function* () {
        const now = yield* Clock.currentTimeMillis;
        const due = yield* store.dueSpaces(now, config.schedulerPageSize);
        for (const state of due) {
          if (running.has(state.space) || stopping.has(state.space)) continue;
          // Store rows are an edge: parse once, then pass the SpaceRef along.
          const ref = yield* parseSpaceRef(state.space).pipe(Effect.option);
          if (Option.isNone(ref)) {
            yield* Effect.logWarning(
              "skipping invalid stored space",
              state.space,
            );
            continue;
          }
          if (
            !(yield* store.claimDue(
              state.space,
              state.nextDueAt,
              now + leaseMs,
            ))
          )
            continue;
          yield* enqueue(ref.value, false);
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
        watch: Effect.fn("SpaceSyncer.watch")(function* (ref: SpaceRef) {
          const space = ref.toString();
          if (Option.isNone(yield* store.getSpace(space))) {
            const now = yield* Clock.currentTimeMillis;
            yield* store.putSpace({
              space,
              authority: ref.spaceDid,
              nextDueAt: now + leaseMs,
              failures: 0,
            });
          }
          yield* enqueue(ref, true);
        }),
        unwatch: Effect.fn("SpaceSyncer.unwatch")(function* (ref: SpaceRef) {
          const space = ref.toString();
          yield* removing(
            ref,
            store
              .removeSpace(space)
              .pipe(Effect.andThen(backoff.clearSpace(space))),
          );
        }),
        notifyWrite: Effect.fn("SpaceSyncer.notifyWrite")(function* (
          ref: SpaceRef,
          spaceRev: string | undefined,
        ) {
          const state = yield* store.getSpace(ref.toString());
          if (Option.isNone(state)) return;
          // Never trusted as a checkpoint: it only decides whether a catch-up pass is worth running.
          if (
            spaceRev &&
            state.value.spaceRev &&
            spaceRev <= state.value.spaceRev
          )
            return;
          yield* enqueue(ref, false);
        }),
        notifySpaceDeleted: Effect.fn("SpaceSyncer.notifySpaceDeleted")(
          function* (ref: SpaceRef) {
            if (Option.isNone(yield* store.getSpace(ref.toString()))) return;
            yield* removing(ref, deleteSpaceData(ref));
          },
        ),
        getBlob: (ref, did, cid) => client.getBlob(ref, did, cid),
        events: Stream.fromPubSub(events),
        // Spaces with a pass running or queued (a finished fiber may linger briefly in the map).
        activeSpaces: Effect.sync(() => running.size),
        awaitIdle: (ref) =>
          Effect.suspend(() => {
            const id = ref.toString();
            if (!running.has(id) && !pending.has(id)) return Effect.void;
            let waiter = idle.get(id);
            if (!waiter) {
              waiter = Deferred.makeUnsafe<void>();
              idle.set(id, waiter);
            }
            return Deferred.await(waiter);
          }),
      });
    }),
  );
}
