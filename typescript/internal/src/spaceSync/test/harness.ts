import { Duration, Effect, Layer, type Scope } from "effect";
import { server } from "../../test/msw";
import { type SpaceSyncOptions, spaceSyncConfigLayer } from "../config";
import { Credentials, DelegationSource } from "../Credentials";
import { CredentialError, errorMessage } from "../errors";
import { Identity } from "../Identity";
import { NotificationAuth } from "../notification";
import { SpaceClient } from "../SpaceClient";
import { RepoSync } from "../repoSync";
import { RepoBackoff, SpacePass } from "../spacePass";
import { SpaceSyncer } from "../SpaceSyncer";
import { SyncSink } from "../SyncSink";
import { SyncStore } from "../SyncStore";
import { FakeNetwork, PLC_URL } from "./fakeNetwork";
import { type RecordingSinkState, makeRecordingSink } from "./recordingSink";

export const SERVICE_DID = "did:web:syncer.test";

/** Short intervals so live tests settle in milliseconds. */
export const testConfig = (overrides: Partial<SpaceSyncOptions> = {}) =>
  spaceSyncConfigLayer({
    serviceDid: SERVICE_DID,
    plcUrl: PLC_URL,
    requestRetryBase: Duration.millis(5),
    backoffBase: Duration.millis(50),
    backoffCap: Duration.millis(200),
    schedulerInterval: Duration.millis(20),
    ...overrides,
  });

export const fakeDelegation = (net: FakeNetwork) =>
  Layer.succeed(
    DelegationSource,
    DelegationSource.of({
      issue: (space) =>
        Effect.tryPromise({
          try: () => net.delegationTokenFor(space),
          catch: (error) =>
            new CredentialError({
              space,
              reason: "NoDelegation",
              message: errorMessage(error),
            }),
        }),
    }),
  );

export type HarnessServices =
  | SyncStore
  | SyncSink
  | SpaceClient
  | Credentials
  | Identity
  | DelegationSource
  | RepoBackoff
  | NotificationAuth
  | RepoSync
  | SpacePass;

export interface Harness {
  readonly net: FakeNetwork;
  readonly sink: RecordingSinkState;
  readonly layer: Layer.Layer<HarnessServices>;
}

export const makeHarness = async (
  overrides: Partial<SpaceSyncOptions> = {},
): Promise<Harness> => {
  const net = new FakeNetwork();
  server.use(...net.handlers);
  const sink = makeRecordingSink();
  const base = Layer.mergeAll(
    SpaceClient.layer.pipe(
      Layer.provideMerge(Credentials.layer),
      Layer.provideMerge(Identity.layer),
      Layer.provideMerge(fakeDelegation(net)),
    ),
    SyncStore.memory,
    sink.layer,
    RepoBackoff.layer,
    NotificationAuth.layer,
  );
  const layer = SpacePass.layer.pipe(
    Layer.provideMerge(RepoSync.layer),
    Layer.provideMerge(base),
    Layer.provide(testConfig(overrides)),
  );
  return { net, sink: sink.state, layer };
};

/** Builds a fresh harness, then runs `body` with its services provided. */
export const runWithHarness = <A, E>(
  body: (h: Harness) => Effect.Effect<A, E, HarnessServices>,
  overrides: Partial<SpaceSyncOptions> = {},
) =>
  Effect.promise(() => makeHarness(overrides)).pipe(
    Effect.flatMap((h) =>
      body(h).pipe(
        Effect.provide(h.layer),
        Effect.provide(testConfig(overrides)),
      ),
    ),
  );

/** Like runWithHarness, with SpaceSyncer (and its scheduler) running for the body. */
export const runWithSyncer = <A, E>(
  body: (
    h: Harness,
  ) => Effect.Effect<A, E, HarnessServices | SpaceSyncer | Scope.Scope>,
  overrides: Partial<SpaceSyncOptions> = {},
) =>
  Effect.promise(() => makeHarness(overrides)).pipe(
    Effect.flatMap((h) =>
      Effect.scoped(
        body(h).pipe(
          Effect.provide(
            SpaceSyncer.layer.pipe(
              Layer.provideMerge(h.layer),
              Layer.provide(testConfig(overrides)),
            ),
          ),
          Effect.provide(testConfig(overrides)),
        ),
      ),
    ),
  );
