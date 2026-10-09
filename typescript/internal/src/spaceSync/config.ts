import { Context, Duration, Layer } from "effect";

export interface SpaceSyncOptions {
  /** Our service identifier, sent to registerNotify and checked as `aud` on notifications. */
  readonly serviceDid: string;
  readonly plcUrl: string;
  /** Max spaces running a pass at once (global). */
  readonly maxActiveSpaces: number;
  /** Max repos synced at once within one space pass. */
  readonly repoConcurrency: number;
  readonly schedulerInterval: Duration.Duration;
  readonly schedulerPageSize: number;
  readonly fullPassInterval: Duration.Duration;
  readonly registrationRenewLead: Duration.Duration;
  readonly backoffBase: Duration.Duration;
  readonly backoffCap: Duration.Duration;
  readonly credentialCacheCapacity: number;
  readonly credentialRefreshLead: Duration.Duration;
  readonly requestRetryBase: Duration.Duration;
  readonly requestRetries: number;
  /** Deadline for one request to return its response headers. */
  readonly requestTimeout: Duration.Duration;
  /** Max gap between chunks of a streamed getRepo CAR. */
  readonly streamIdleTimeout: Duration.Duration;
  /** Ops one incremental sync will buffer before falling back to getRepo. */
  readonly maxIncrementalOps: number;
  /** Events kept for slow `events` subscribers; the oldest are dropped past this. */
  readonly eventBufferSize: number;
}

export const defaultSpaceSyncOptions: SpaceSyncOptions = {
  serviceDid: "",
  plcUrl: "https://plc.directory",
  maxActiveSpaces: 64,
  repoConcurrency: 8,
  schedulerInterval: Duration.seconds(30),
  schedulerPageSize: 500,
  fullPassInterval: Duration.hours(6),
  registrationRenewLead: Duration.hours(1),
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

export const spaceSyncConfigLayer = (
  options: Partial<SpaceSyncOptions> & { readonly serviceDid: string },
) => Layer.succeed(SpaceSyncConfig, { ...defaultSpaceSyncOptions, ...options });
