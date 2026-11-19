import type { IdentityResolver } from "@atproto-labs/identity-resolver";
import { Context, Duration, Layer } from "effect";

export interface SpaceSyncOptions {
  /**
   * Base URL of this syncer's notification endpoint, which serves
   * `com.atproto.space.confirmRegistration` and `notifySpaceUpdate`. When set,
   * every space credential is requested with a registration for it, so the
   * space host tells us about changes for as long as the credential is valid.
   * Unset, the syncer only polls.
   */
  readonly notifyEndpoint?: string;
  /**
   * Secret the notification tokens are derived from. Set it to keep
   * registrations valid across restarts and across instances behind one
   * endpoint; unset, a random one is generated per process.
   */
  readonly notifySecret?: string;
  /**
   * Resolves the DIDs of space authorities and repos. Defaults to the public
   * did:plc / did:web network; pass e.g. a HabitatIdentityResolver to resolve
   * identities the public network doesn't know.
   */
  readonly identityResolver?: IdentityResolver;
  /** Max spaces running a pass at once (global). */
  readonly maxActiveSpaces: number;
  /** Max repos synced at once within one space pass. */
  readonly repoConcurrency: number;
  readonly schedulerInterval: Duration.Duration;
  readonly schedulerPageSize: number;
  readonly fullPassInterval: Duration.Duration;
  readonly backoffBase: Duration.Duration;
  readonly backoffCap: Duration.Duration;
  readonly credentialCacheCapacity: number;
  readonly credentialRefreshLead: Duration.Duration;
  readonly requestRetryBase: Duration.Duration;
  readonly requestRetries: number;
  /** Deadline for one request to return its response headers. */
  readonly requestTimeout: Duration.Duration;
  /** Max gap between chunks of a streamed getRepo STAR archive. */
  readonly streamIdleTimeout: Duration.Duration;
  /** Ops one incremental sync will buffer before falling back to getRepo. */
  readonly maxIncrementalOps: number;
  /** Events kept for slow `events` subscribers; the oldest are dropped past this. */
  readonly eventBufferSize: number;
}

export const defaultSpaceSyncOptions: SpaceSyncOptions = {
  maxActiveSpaces: 64,
  repoConcurrency: 8,
  schedulerInterval: Duration.seconds(30),
  schedulerPageSize: 500,
  fullPassInterval: Duration.hours(6),
  backoffBase: Duration.seconds(30),
  backoffCap: Duration.hours(1),
  credentialCacheCapacity: 1024,
  credentialRefreshLead: Duration.seconds(30),
  requestRetryBase: Duration.millis(500),
  requestRetries: 2,
  requestTimeout: Duration.seconds(30),
  streamIdleTimeout: Duration.seconds(60),
  maxIncrementalOps: 10_000,
  eventBufferSize: 10_000,
};

export const SpaceSyncConfig = Context.Reference<SpaceSyncOptions>(
  "internal/spaceSync/SpaceSyncConfig",
  { defaultValue: () => defaultSpaceSyncOptions },
);

export const spaceSyncConfigLayer = (options: Partial<SpaceSyncOptions> = {}) =>
  Layer.succeed(SpaceSyncConfig, { ...defaultSpaceSyncOptions, ...options });
