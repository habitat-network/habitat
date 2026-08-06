export type HabitatIdentityResolverErrorOptions = {
  cause?: unknown;
  /** HTTP status returned by the Habitat instance, when there was a response. */
  status?: number;
  /** The XRPC `error` field, e.g. "DidNotFound" or "HandleNotFound". */
  xrpcError?: string;
};

/**
 * Raised for every failure originating from this package: unreachable host,
 * non-2xx XRPC response, or a malformed identity payload.
 *
 * Abort errors are deliberately *not* wrapped in this type, so callers can keep
 * distinguishing cancellation from failure.
 */
export class HabitatIdentityResolverError extends Error {
  readonly status?: number;
  readonly xrpcError?: string;

  constructor(
    message: string,
    options: HabitatIdentityResolverErrorOptions = {},
  ) {
    super(message, { cause: options.cause });
    this.name = "HabitatIdentityResolverError";
    this.status = options.status;
    this.xrpcError = options.xrpcError;
  }
}
