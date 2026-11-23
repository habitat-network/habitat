import { lexParse } from "@atproto/lex-json";
import { Effect, Predicate, Schema } from "effect";
import { FrameDecodeError } from "./errors";

const NSID = "network.habitat.space.subscribe";

/** What every commit event carries, whether a write or a delete. */
interface CommitFields {
  /** The host's event sequence number: the stream cursor. */
  readonly seq: number;
  /** The space the record belongs to. */
  readonly space: string;
  /** The record's author: the repo that holds it. */
  readonly did: string;
  /** The record's space record URI. */
  readonly uri: string;
  readonly collection: string;
  readonly rkey: string;
  /** The repo revision of the write. */
  readonly rev: string;
  /** When the write was stored. */
  readonly time: string;
}

/** A record was created or updated. Handled by a `put` handler. */
export interface PutEvent<R = Record<string, unknown>> extends CommitFields {
  readonly kind: "put";
  readonly operation: "create" | "update";
  readonly cid: string;
  /** The record, decoded from JSON: blob refs, bytes and CIDs are typed. */
  readonly record: R;
}

/** A record was deleted. Handled by a `del` handler. */
export interface DelEvent extends CommitFields {
  readonly kind: "del";
  readonly operation: "delete";
}

/** A space was deleted: drop everything held for it. */
export interface SpaceDeletedEvent {
  readonly kind: "spaceDeleted";
  readonly seq: number;
  readonly space: string;
  readonly time: string;
}

export type CommitEvent = PutEvent | DelEvent;
export type SpaceEvent = CommitEvent | SpaceDeletedEvent;

/** A decoded xrpc.v1.json frame of network.habitat.space.subscribe. */
export type Frame =
  | { readonly _tag: "Event"; readonly event: SpaceEvent }
  | { readonly _tag: "Info"; readonly name: string; readonly message?: string }
  | { readonly _tag: "Error"; readonly error: string; readonly message: string }
  /** A message of a kind this client doesn't know: skipped. */
  | { readonly _tag: "Unknown" };

// The wire shapes, as the subscribe lexicon defines them. Decoding them with
// Schema is what validates a frame; the record body stays Unknown here and is
// re-parsed as lex JSON afterwards.
const Fields = {
  seq: Schema.Int,
  space: Schema.String,
  time: Schema.String,
};

const CommitPayload = Schema.Struct({
  ...Fields,
  did: Schema.String,
  uri: Schema.String,
  collection: Schema.String,
  rkey: Schema.String,
  rev: Schema.String,
  operation: Schema.String,
  cid: Schema.optional(Schema.String),
  record: Schema.optional(Schema.Record(Schema.String, Schema.Unknown)),
});

const SpaceDeletedPayload = Schema.Struct(Fields);

const InfoPayload = Schema.Struct({
  name: Schema.String,
  message: Schema.optional(Schema.String),
});

const ErrorFrame = Schema.Struct({
  error: Schema.String,
  message: Schema.optional(Schema.String),
});

const Envelope = Schema.Struct({
  $type: Schema.String,
  payload: Schema.optional(Schema.Unknown),
});

const SnapshotPayload = Schema.Struct({
  events: Schema.Array(Schema.Unknown),
  plannedThroughSeq: Schema.Int,
  tipSeq: Schema.Int,
});

const unknown: Frame = { _tag: "Unknown" };

const event = (event: SpaceEvent): Frame => ({ _tag: "Event", event });

// decodePayload decodes one payload object by its $type. A kind this client
// doesn't know is Unknown (skipped), so the server can add event types
// without breaking old clients; a known kind that doesn't fit its shape fails.
function decodePayload(payload: unknown) {
  const type = Predicate.hasProperty(payload, "$type") ? payload.$type : "";
  switch (type) {
    case `${NSID}#commit`:
      return Schema.decodeUnknownEffect(CommitPayload)(payload).pipe(
        Effect.flatMap(({ operation, cid, record, ...fields }) => {
          if (operation === "delete") {
            return Effect.succeed(event({ ...fields, kind: "del", operation }));
          }
          if (operation !== "create" && operation !== "update") {
            return Effect.succeed(unknown);
          }
          if (cid === undefined || record === undefined) {
            return Effect.fail(
              new Error(`${operation} without cid and record`),
            );
          }
          // Re-parse the record alone as lex JSON, so {$link}, {$bytes} and
          // blob refs come out typed, the way records read from a repo do.
          return Effect.try(() =>
            event({
              ...fields,
              kind: "put",
              operation,
              cid,
              record: lexParse(JSON.stringify(record)) as Record<
                string,
                unknown
              >,
            }),
          );
        }),
      );
    case `${NSID}#spaceDeleted`:
      return Schema.decodeUnknownEffect(SpaceDeletedPayload)(payload).pipe(
        Effect.map((e) => event({ ...e, kind: "spaceDeleted" })),
      );
    case `${NSID}#info`:
      return Schema.decodeUnknownEffect(InfoPayload)(payload).pipe(
        Effect.map((info): Frame => ({ _tag: "Info", ...info })),
      );
    default:
      return Effect.succeed(unknown);
  }
}

const failed = (what: string) => (cause: unknown) =>
  new FrameDecodeError({
    message: `invalid ${what}: ${cause instanceof Error ? cause.message : String(cause)}`,
  });

/**
 * decodeEvent decodes one event object, the payload of a subscribe message or
 * an entry of getSnapshot's events, which share their shape. Kinds this
 * client doesn't know are undefined.
 */
export const decodeEvent = (
  payload: unknown,
): Effect.Effect<SpaceEvent | undefined, FrameDecodeError> =>
  decodePayload(payload).pipe(
    Effect.map((frame) => (frame._tag === "Event" ? frame.event : undefined)),
    Effect.mapError(failed("event")),
  );

/** decodeFrame parses one text frame of the subscribe stream. */
export const decodeFrame = (
  text: string,
): Effect.Effect<Frame, FrameDecodeError> =>
  Effect.gen(function* () {
    const raw: unknown = yield* Effect.try(() => JSON.parse(text));
    const frame = yield* Schema.decodeUnknownEffect(Envelope)(raw);
    if (frame.$type === "error") {
      const e = yield* Schema.decodeUnknownEffect(ErrorFrame)(raw);
      return {
        _tag: "Error",
        error: e.error,
        message: e.message ?? "",
      } as const;
    }
    if (frame.$type !== "message") {
      return yield* Effect.fail(
        new Error("frame is neither a message nor an error"),
      );
    }
    return yield* decodePayload(frame.payload);
  }).pipe(Effect.mapError(failed("subscribe frame")));

/**
 * decodeSnapshotPage decodes a getSnapshot response: its events (skipping
 * kinds this client doesn't know) and its seq bounds.
 */
export const decodeSnapshotPage = (body: unknown) =>
  Schema.decodeUnknownEffect(SnapshotPayload)(body).pipe(
    Effect.mapError(failed("getSnapshot response")),
    Effect.flatMap((page) =>
      Effect.forEach(page.events, decodeEvent).pipe(
        Effect.map((events) => ({
          events: events.filter((e) => e !== undefined),
          plannedThroughSeq: page.plannedThroughSeq,
          tipSeq: page.tipSeq,
        })),
      ),
    ),
  );
