import { Context, type Effect } from "effect";
import type { SinkError } from "./errors";
import type { RepoBatch } from "./types";

/**
 * Host-provided: applies verified batches to the host's index. Delivery is
 * at-least-once, so apply must be idempotent (key on uri + cid). apply may be
 * interrupted (e.g. unwatch during a Reset) and must roll back when it is.
 */
export class SyncSink extends Context.Service<
  SyncSink,
  { readonly apply: (batch: RepoBatch) => Effect.Effect<void, SinkError> }
>()("internal/spaceSync/SyncSink") {}
