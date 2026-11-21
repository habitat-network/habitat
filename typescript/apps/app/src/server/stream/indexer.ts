import { parseCid, type LexMap } from "@atproto/lex";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import type {
  CommitEvent,
  EventSink,
  SpaceDeletedEvent,
  SpaceEvent,
} from "@habitat-network/jetstream";
import { Effect, Schema, Sink } from "effect";
import type { RepoBatch, SyncSink } from "./batch";

// The collections the app indexes from its groups' streams: docs (crdt
// state, rendered markdown, comments and replies, sharing grants), uploaded
// files, deployed apps, and group rosters. The stream is asked for only these. Spelled
// out rather than imported from the sinks, so the Worker entry (which
// exports OrgStream) doesn't load them until a stream starts.
export const INDEXED_COLLECTIONS = [
  "network.habitat.docs.crdt",
  "network.habitat.docs.markdown",
  "network.habitat.docs.comment",
  "network.habitat.docs.commentReply",
  "network.habitat.relationship.userRelation",
  "network.habitat.relationship.spaceRelation",
  "network.habitat.drop.file",
  "network.habitat.apps.app",
  "community.opensocial.membership",
] as const;

// batchOf turns one stream event into the one-change batch the app's sinks
// apply. A stream event is one record change.
export function batchOf(event: CommitEvent | SpaceDeletedEvent): RepoBatch {
  if (event.kind === "spaceDeleted") {
    return { _tag: "SpaceDeleted", space: event.space as SpaceRefString };
  }
  return {
    _tag: "Ops",
    space: event.space as SpaceRefString,
    did: event.did as DidString,
    rev: event.rev,
    changes: [
      {
        uri: event.uri,
        collection: event.collection,
        rkey: event.rkey,
        cid: event.kind === "put" ? parseCid(event.cid) : null,
        value: event.kind === "put" ? (event.record as LexMap) : undefined,
      },
    ],
  };
}

// batchesOf turns a group of stream events, in order, into the batches the
// sinks apply. Adjacent writes by one repo to one space merge into a single
// Ops batch, and within it a record written more than once keeps only its
// last change (the earlier ones are superseded), so a sink can write a whole
// group at once.
export function batchesOf(events: ReadonlyArray<SpaceEvent>): RepoBatch[] {
  const batches: RepoBatch[] = [];
  for (const event of events) {
    if (
      event.kind !== "spaceDeleted" &&
      !(INDEXED_COLLECTIONS as readonly string[]).includes(event.collection)
    ) {
      continue;
    }
    const batch = batchOf(event);
    const prev = batches[batches.length - 1];
    if (
      prev?._tag === "Ops" &&
      batch._tag === "Ops" &&
      prev.space === batch.space &&
      prev.did === batch.did
    ) {
      batches[batches.length - 1] = {
        ...batch,
        changes: [...prev.changes, ...batch.changes],
      };
    } else {
      batches.push(batch);
    }
  }
  return batches.map((batch) => {
    if (batch._tag !== "Ops") return batch;
    const last = new Map(batch.changes.map((c, i) => [c.uri, i]));
    return {
      ...batch,
      changes: batch.changes.filter((c, i) => last.get(c.uri) === i),
    };
  });
}

// SinkError: the app's sink couldn't apply a batch. The runner retries.
export class SinkError extends Schema.TaggedError<SinkError>()("SinkError", {
  message: Schema.String,
  cause: Schema.Defect(),
}) {}

// appEventSink is what the group's stream runner feeds: an Effect Sink over
// stream events that applies each group to sink. The sink's promise is an
// Effect here, so a failed apply is a typed SinkError and an interrupted run
// aborts the apply's signal.
export function appEventSink(sink: SyncSink): EventSink<SinkError> {
  return Sink.forEachArray((events: ReadonlyArray<SpaceEvent>) =>
    Effect.forEach(
      batchesOf(events),
      (batch) =>
        Effect.tryPromise({
          try: (signal) => sink.apply(batch, signal),
          catch: (cause) =>
            new SinkError({
              message: cause instanceof Error ? cause.message : String(cause),
              cause,
            }),
        }),
      { discard: true },
    ),
  );
}
