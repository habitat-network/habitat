import { Duration, Effect, Layer } from "effect";
import { type SpaceSyncOptions, spaceSyncConfigLayer } from "../config";
import { DelegationSource } from "../Credentials";
import { CredentialError, errorMessage } from "../errors";
import { type FakeNetwork, PLC_URL } from "./fakeNetwork";

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
          catch: (error) => new CredentialError({ space, reason: "NoDelegation", message: errorMessage(error) }),
        }),
    }),
  );