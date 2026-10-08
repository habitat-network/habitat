import { Predicate, Schema } from "effect";

export class InvalidSpaceRefError extends Schema.TaggedError<InvalidSpaceRefError>()(
  "InvalidSpaceRefError",
  { space: Schema.String },
) {}

/** A body did not match its lexicon schema. */
export class WireDecodeError extends Schema.TaggedError<WireDecodeError>()(
  "WireDecodeError",
  { message: Schema.String },
) {}

export class IdentityError extends Schema.TaggedError<IdentityError>()(
  "IdentityError",
  {
    did: Schema.String,
    message: Schema.String,
  },
) {}

/**
 * Failure to obtain or use a space credential. Only `SpaceDeleted` says anything
 * about the space itself (proposal: "A renewal that fails for any other reason
 * says nothing about the space").
 */
export class CredentialError extends Schema.TaggedError<CredentialError>()(
  "CredentialError",
  {
    space: Schema.String,
    reason: Schema.Literals([
      "SpaceDeleted",
      "SpaceNotFound",
      "NotAuthorized",
      "NoDelegation",
      "Transport",
    ]),
    message: Schema.String,
  },
) {}

/** A non-2xx (or unreadable) XRPC response. `status: 0` means the request never completed. */
export class XrpcError extends Schema.TaggedError<XrpcError>()("XrpcError", {
  method: Schema.String,
  status: Schema.Number,
  error: Schema.optional(Schema.String),
  message: Schema.String,
}) {}

/** Internal: a repo failed verification; triggers full-state recovery. */
export class RepoVerificationError extends Schema.TaggedError<RepoVerificationError>()(
  "RepoVerificationError",
  { space: Schema.String, did: Schema.String, message: Schema.String },
) {}

/** Recovery itself failed; the repo is retried on a later pass. */
export class RepoSyncError extends Schema.TaggedError<RepoSyncError>()(
  "RepoSyncError",
  {
    space: Schema.String,
    did: Schema.String,
    message: Schema.String,
    cause: Schema.Defect(),
  },
) {}

export class SinkError extends Schema.TaggedError<SinkError>()("SinkError", {
  message: Schema.String,
  cause: Schema.Defect(),
}) {}

export class StoreError extends Schema.TaggedError<StoreError>()("StoreError", {
  message: Schema.String,
  cause: Schema.Defect(),
}) {}

export class NotificationAuthError extends Schema.TaggedError<NotificationAuthError>()(
  "NotificationAuthError",
  { message: Schema.String },
) {}

export const errorMessage = (error: unknown): string =>
  Predicate.hasProperty(error, "message") && Predicate.isString(error.message)
    ? error.message
    : String(error);
