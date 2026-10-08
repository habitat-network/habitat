import type { DidString, SpaceRef, SpaceRefString } from "@atproto/syntax";
import type { VerifiedRecord } from "@atproto/space";
import { Effect, Layer, ManagedRuntime, Option, Stream } from "effect";
import { type SpaceSyncOptions, spaceSyncConfigLayer } from "./config";
import { Credentials, DelegationSource } from "./Credentials";
import { CredentialError, SinkError, StoreError, errorMessage } from "./errors";
import { Identity } from "./Identity";
import {
  NotificationAuth,
  receiveNotifySpaceDeleted,
  receiveNotifyWrite,
} from "./notification";
import { SpaceClient } from "./SpaceClient";
import { RepoBackoff } from "./spacePass";
import { SpaceSyncer } from "./SpaceSyncer";
import { SyncSink } from "./SyncSink";
import { SyncStore } from "./SyncStore";
import type { RepoBatch, RepoState, SpaceState, SyncEvent } from "./types";
import { parseSpaceRef } from "./wire";

export interface PromiseSyncStore {
  getSpace(space: SpaceRefString): Promise<SpaceState | undefined>;
  putSpace(state: SpaceState): Promise<void>;
  removeSpace(space: SpaceRefString): Promise<void>;
  /** Set nextDueAt to `until` only if it still equals `seen`; atomically. */
  claimDue(
    space: SpaceRefString,
    seen: number,
    until: number,
  ): Promise<boolean>;
  dueSpaces(now: number, limit: number): Promise<ReadonlyArray<SpaceState>>;
  getRepo(
    space: SpaceRefString,
    did: DidString,
  ): Promise<RepoState | undefined>;
  listRepoDids(space: SpaceRefString): Promise<ReadonlyArray<DidString>>;
  putRepo(state: RepoState): Promise<void>;
  removeRepo(space: SpaceRefString, did: DidString): Promise<void>;
}

export type PromiseRepoBatch =
  | Exclude<RepoBatch, { _tag: "Reset" }>
  | (Omit<Extract<RepoBatch, { _tag: "Reset" }>, "records"> & {
      readonly records: AsyncIterable<VerifiedRecord>;
    });

export interface PromiseSyncSink {
  /** Must drain Reset.records, be idempotent, and roll back if `signal` aborts. */
  apply(batch: PromiseRepoBatch, signal: AbortSignal): Promise<void>;
}

export interface PromiseDelegationSource {
  issue(space: SpaceRefString): Promise<string>;
  attestation?(space: SpaceRefString, aud: string): Promise<string>;
}

export interface CreateSpaceSyncerOptions extends Partial<SpaceSyncOptions> {
  readonly serviceDid: string;
  readonly store: PromiseSyncStore;
  readonly sink: PromiseSyncSink;
  readonly delegation: PromiseDelegationSource;
}

const storeLayer = (store: PromiseSyncStore) => {
  const wrap = <A>(f: () => Promise<A>) =>
    Effect.tryPromise({
      try: f,
      catch: (cause) => new StoreError({ message: errorMessage(cause), cause }),
    });
  return Layer.succeed(
    SyncStore,
    SyncStore.of({
      getSpace: (space) =>
        wrap(() => store.getSpace(space)).pipe(
          Effect.map(Option.fromNullishOr),
        ),
      putSpace: (state) => wrap(() => store.putSpace(state)),
      removeSpace: (space) => wrap(() => store.removeSpace(space)),
      claimDue: (space, seen, until) =>
        wrap(() => store.claimDue(space, seen, until)),
      dueSpaces: (now, limit) => wrap(() => store.dueSpaces(now, limit)),
      getRepo: (space, did) =>
        wrap(() => store.getRepo(space, did)).pipe(
          Effect.map(Option.fromNullishOr),
        ),
      listRepoDids: (space) => wrap(() => store.listRepoDids(space)),
      putRepo: (state) => wrap(() => store.putRepo(state)),
      removeRepo: (space, did) => wrap(() => store.removeRepo(space, did)),
    }),
  );
};

const sinkLayer = (sink: PromiseSyncSink) =>
  Layer.succeed(
    SyncSink,
    SyncSink.of({
      apply: (batch) =>
        Effect.tryPromise({
          try: (signal) =>
            sink.apply(
              batch._tag === "Reset"
                ? { ...batch, records: Stream.toAsyncIterable(batch.records) }
                : batch,
              signal,
            ),
          catch: (cause) =>
            new SinkError({ message: errorMessage(cause), cause }),
        }),
    }),
  );

const delegationLayer = (delegation: PromiseDelegationSource) => {
  const { attestation } = delegation;
  return Layer.succeed(
    DelegationSource,
    DelegationSource.of({
      issue: (space) =>
        Effect.tryPromise({
          try: () => delegation.issue(space),
          catch: (e) =>
            new CredentialError({
              space,
              reason: "NoDelegation",
              message: errorMessage(e),
            }),
        }),
      attestation:
        attestation &&
        ((space, aud) =>
          Effect.tryPromise({
            try: () => attestation(space, aud),
            catch: (e) =>
              new CredentialError({
                space,
                reason: "NotAuthorized",
                message: errorMessage(e),
              }),
          })),
    }),
  );
};

/** Promise API over SpaceSyncer for hosts that don't use Effect. Call dispose() on shutdown. */
export const createSpaceSyncer = (options: CreateSpaceSyncerOptions) => {
  const { store, sink, delegation, ...config } = options;
  const configLayer = spaceSyncConfigLayer(config);
  const layer = Layer.mergeAll(SpaceSyncer.layer, NotificationAuth.layer).pipe(
    Layer.provideMerge(RepoBackoff.layer),
    Layer.provideMerge(SpaceClient.layer),
    Layer.provideMerge(Credentials.layer),
    Layer.provideMerge(Identity.layer),
    Layer.provideMerge(
      Layer.mergeAll(
        storeLayer(store),
        sinkLayer(sink),
        delegationLayer(delegation),
      ),
    ),
    Layer.provideMerge(configLayer),
  );
  const runtime = ManagedRuntime.make(layer);
  const run = <A, E>(
    effect: Effect.Effect<A, E, Layer.Success<typeof layer>>,
  ) => runtime.runPromise(effect);
  /** Validates the space once; the syncer works with the parsed SpaceRef. */
  const withSpace = <A, E>(
    space: string,
    f: (
      syncer: SpaceSyncer["Service"],
      ref: SpaceRef,
    ) => Effect.Effect<A, E, Layer.Success<typeof layer>>,
  ) =>
    run(
      Effect.gen(function* () {
        const ref = yield* parseSpaceRef(space);
        return yield* f(yield* SpaceSyncer, ref);
      }),
    );

  return {
    watch: (space: string) => withSpace(space, (s, ref) => s.watch(ref)),
    unwatch: (space: string) => withSpace(space, (s, ref) => s.unwatch(ref)),
    /** Pass the raw JSON body and Authorization header of an inbound notifyWrite. */
    notifyWrite: (body: unknown, authorization: string | undefined) =>
      run(receiveNotifyWrite(body, authorization)),
    /** Pass the raw JSON body and Authorization header of an inbound notifySpaceDeleted. */
    notifySpaceDeleted: (body: unknown, authorization: string | undefined) =>
      run(receiveNotifySpaceDeleted(body, authorization)),
    getBlob: (space: string, did: DidString, cid: string) =>
      withSpace(space, (s, ref) => s.getBlob(ref, did, cid)),
    awaitIdle: (space: string) =>
      withSpace(space, (s, ref) => s.awaitIdle(ref)),
    // `Stream.provide(runtime)` is not a Layer in v4, so resolve the service once
    // and hand back its stream.
    events: (): AsyncIterable<SyncEvent> => ({
      async *[Symbol.asyncIterator]() {
        const syncer = await runtime.runPromise(
          Effect.flatMap(SpaceSyncer, Effect.succeed),
        );
        yield* Stream.toAsyncIterable(syncer.events);
      },
    }),
    dispose: () => runtime.dispose(),
  };
};

export type PromiseSpaceSyncer = ReturnType<typeof createSpaceSyncer>;
