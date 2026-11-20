import { parseCid, type LexMap } from "@atproto/lex";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import {
  LexIndexer,
  type CommitEvent,
  type SpaceDeletedEvent,
} from "@habitat-network/jetstream";
import type { PromiseRepoBatch, PromiseSyncSink } from "internal/spaceSync";

// The collections the app indexes from its groups' streams: docs (crdt
// state, rendered markdown, comments and replies, sharing grants), uploaded
// files, and group rosters. The stream is asked for only these. Spelled
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
  "community.opensocial.membership",
] as const;

// batchOf turns one stream event into the one-change batch the app's sinks
// apply. The sinks were written for internal/spaceSync's per-repo batches;
// a stream event is the same change, one record at a time.
export function batchOf(
  event: CommitEvent | SpaceDeletedEvent,
): PromiseRepoBatch {
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

// appIndexer routes every indexed collection, and space deletions, to sink.
export function appIndexer(sink: PromiseSyncSink): LexIndexer {
  const apply = (event: CommitEvent | SpaceDeletedEvent) =>
    sink.apply(batchOf(event), new AbortController().signal);
  const indexer = new LexIndexer({ mode: "spaces" });
  for (const collection of INDEXED_COLLECTIONS) {
    indexer.commit(collection, { put: apply, del: apply });
  }
  return indexer.spaceDeleted(apply);
}
