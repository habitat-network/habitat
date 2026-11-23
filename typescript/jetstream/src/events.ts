import { lexParse } from "@atproto/lex-json";
import { Effect } from "effect";
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

const isObject = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);

const str = (o: Record<string, unknown>, k: string): string => {
  const v = o[k];
  if (typeof v !== "string") throw new Error(`missing ${k}`);
  return v;
};

const num = (o: Record<string, unknown>, k: string): number => {
  const v = o[k];
  if (typeof v !== "number" || !Number.isSafeInteger(v)) {
    throw new Error(`missing ${k}`);
  }
  return v;
};

function decodePayload(payload: Record<string, unknown>): Frame {
  switch (payload.$type) {
    case `${NSID}#commit`: {
      const fields: CommitFields = {
        seq: num(payload, "seq"),
        space: str(payload, "space"),
        did: str(payload, "did"),
        uri: str(payload, "uri"),
        collection: str(payload, "collection"),
        rkey: str(payload, "rkey"),
        rev: str(payload, "rev"),
        time: str(payload, "time"),
      };
      const operation = str(payload, "operation");
      if (operation === "delete") {
        return { _tag: "Event", event: { ...fields, kind: "del", operation } };
      }
      if (operation !== "create" && operation !== "update") {
        return { _tag: "Unknown" };
      }
      if (!isObject(payload.record)) throw new Error("missing record");
      // Re-parse the record alone as lex JSON, so {$link}, {$bytes} and
      // blob refs come out typed, the way records read from a repo do.
      const record = lexParse(JSON.stringify(payload.record)) as Record<
        string,
        unknown
      >;
      return {
        _tag: "Event",
        event: {
          ...fields,
          kind: "put",
          operation,
          cid: str(payload, "cid"),
          record,
        },
      };
    }
    case `${NSID}#spaceDeleted`:
      return {
        _tag: "Event",
        event: {
          kind: "spaceDeleted",
          seq: num(payload, "seq"),
          space: str(payload, "space"),
          time: str(payload, "time"),
        },
      };
    case `${NSID}#info`:
      return {
        _tag: "Info",
        name: str(payload, "name"),
        ...(typeof payload.message === "string"
          ? { message: payload.message }
          : {}),
      };
    default:
      return { _tag: "Unknown" };
  }
}

/**
 * decodeEvent decodes one event object, the payload of a subscribe message or
 * an entry of getSnapshot's events, which share their shape. Kinds this
 * client doesn't know are undefined.
 */
export function decodeEvent(payload: unknown): SpaceEvent | undefined {
  if (!isObject(payload)) throw new Error("event is not an object");
  const frame = decodePayload(payload);
  return frame._tag === "Event" ? frame.event : undefined;
}

/** decodeFrame parses one text frame of the subscribe stream. */
export const decodeFrame = (
  text: string,
): Effect.Effect<Frame, FrameDecodeError> =>
  Effect.try({
    try: (): Frame => {
      const frame: unknown = JSON.parse(text);
      if (!isObject(frame)) throw new Error("frame is not an object");
      if (frame.$type === "error") {
        return {
          _tag: "Error",
          error: str(frame, "error"),
          message: typeof frame.message === "string" ? frame.message : "",
        };
      }
      if (frame.$type !== "message" || !isObject(frame.payload)) {
        throw new Error("frame is neither a message nor an error");
      }
      return decodePayload(frame.payload);
    },
    catch: (cause) =>
      new FrameDecodeError({
        message: `invalid subscribe frame: ${cause instanceof Error ? cause.message : String(cause)}`,
      }),
  });
