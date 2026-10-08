import type { VerifiedRecord } from "@atproto/space";
import { Effect, Layer, ManagedRuntime, Option, Schema, Stream } from "effect";
import { type SpaceSyncOptions, spaceSyncConfigLayer } from "./config";
import { Credentials, DelegationSource } from "./Credentials";
import { CredentialError, SinkError, StoreError, errorMessage } from "./errors";
import { Identity } from "./Identity";
import { verifyNotification } from "./notification";
import { SpaceClient } from "./SpaceClient";
import { SpaceSyncer } from "./SpaceSyncer";
import { SyncSink } from "./SyncSink";
import { SyncStore } from "./SyncStore";
import type { RepoBatch, RepoState, SpaceState, SyncEvent } from "./types";
import { NotifySpaceDeletedInput, NotifyWriteInput, lexJson } from "./wire";

export interface PromiseSyncStore {
  getSpace(space: string): Promise<SpaceState | undefined>;
  putSpace(state: SpaceState): Promise<void>;
  removeSpace(space: string): Promise<void>;
  dueSpaces(now: number, limit: number): Promise<ReadonlyArray<SpaceState>>;
  getRepo(space: string, did: string): Promise<RepoState | undefined>;
  listRepoDids(space: string): Promise<ReadonlyArray<string>>;
  putRepo(state: RepoState): Promise<void>;
  removeRepo(space: string, did: string): Promise<void>;
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
  issue(space: string): Promise<string>;
  attestation?(space: string, aud: string): Promise<string>;
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

const delegationLayer = (delegation: PromiseDelegationSource) =>
  Layer.succeed(
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
      ...(delegation.attestation
        ? {
            attestation: (space: string, aud: string) =>
              Effect.tryPromise({
                try: () => delegation.attestation!(space, aud),
                catch: (e) =>
                  new CredentialError({
                    space,
                    reason: "NotAuthorized",
                    message: errorMessage(e),
                  }),
              }),
          }
        : {}),
    }),
  );

/** Promise API over SpaceSyncer for hosts that don't use Effect. Call dispose() on shutdown. */
export const createSpaceSyncer = (options: CreateSpaceSyncerOptions) => {
  const { store, sink, delegation, ...config } = options;
  const configLayer = spaceSyncConfigLayer(config);
  const layer = SpaceSyncer.layer.pipe(
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
  const run = <A, E>(effect: Effect.Effect<A, E, SpaceSyncer | Identity>) =>
    runtime.runPromise(effect);

  return {
    watch: (space: string) =>
      run(Effect.flatMap(SpaceSyncer, (s) => s.watch(space))),
    unwatch: (space: string) =>
      run(Effect.flatMap(SpaceSyncer, (s) => s.unwatch(space))),
    /** Pass the raw JSON body and Authorization header of an inbound notifyWrite. */
    notifyWrite: (body: unknown, authorization: string | undefined) =>
      run(
        Effect.gen(function* () {
          const input = yield* Schema.decodeUnknownEffect(NotifyWriteInput)(
            lexJson(body),
          );
          yield* verifyNotification(authorization, {
            lxm: "com.atproto.space.notifyWrite",
            space: input.space,
            serviceDid: options.serviceDid,
          });
          yield* Effect.flatMap(SpaceSyncer, (s) => s.notifyWrite(input));
        }),
      ),
    notifySpaceDeleted: (body: unknown, authorization: string | undefined) =>
      run(
        Effect.gen(function* () {
          const { space } = yield* Schema.decodeUnknownEffect(
            NotifySpaceDeletedInput,
          )(body);
          yield* verifyNotification(authorization, {
            lxm: "com.atproto.space.notifySpaceDeleted",
            space,
            serviceDid: options.serviceDid,
          });
          yield* Effect.flatMap(SpaceSyncer, (s) =>
            s.notifySpaceDeleted(space),
          );
        }),
      ),
    getBlob: (space: string, did: string, cid: string) =>
      run(Effect.flatMap(SpaceSyncer, (s) => s.getBlob(space, did, cid))),
    awaitIdle: (space: string) =>
      run(Effect.flatMap(SpaceSyncer, (s) => s.awaitIdle(space))),
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
