import { Schema } from "effect";

/** The stream's connection could not be opened, or was lost. Retried. */
export class ConnectionError extends Schema.TaggedError<ConnectionError>()(
  "ConnectionError",
  {
    message: Schema.String,
    /** The HTTP status the upgrade was refused with, if it was. */
    status: Schema.optional(Schema.Number),
  },
) {}

/**
 * The server ended the stream with an error frame (xrpc.v1.json). An
 * ExpiredToken is retried with fresh credentials; others are not.
 */
export class StreamError extends Schema.TaggedError<StreamError>()(
  "StreamError",
  { error: Schema.String, message: Schema.String },
) {}

/** A frame that isn't a valid subscribe message. */
export class FrameDecodeError extends Schema.TaggedError<FrameDecodeError>()(
  "FrameDecodeError",
  { message: Schema.String },
) {}

/** A handler failed; the event is retried, so the cursor never skips it. */
export class HandlerError extends Schema.TaggedError<HandlerError>()(
  "HandlerError",
  { uri: Schema.String, message: Schema.String, cause: Schema.Defect() },
) {}

/** The cursor store failed to read or save the cursor. */
export class CursorError extends Schema.TaggedError<CursorError>()(
  "CursorError",
  { message: Schema.String, cause: Schema.Defect() },
) {}

/** The write itself was refused by the writer's host. Nothing was written. */
export class WriteError extends Schema.TaggedError<WriteError>()("WriteError", {
  nsid: Schema.String,
  status: Schema.Number,
  error: Schema.optional(Schema.String),
  message: Schema.String,
}) {}

/**
 * The write succeeded, but its event wasn't indexed within the timeout: the
 * stream is behind or disconnected, or a later write to the same record
 * replaced it in the compacted log first. The record is written; it will be
 * indexed when the stream catches up.
 */
export class WriteNotObserved extends Schema.TaggedError<WriteNotObserved>()(
  "WriteNotObserved",
  { uri: Schema.String, cid: Schema.optional(Schema.String) },
) {}

export const errorMessage = (cause: unknown): string =>
  cause instanceof Error ? cause.message : String(cause);
