import type { LexMap } from "@atproto/lex";
import type {
  CommitHandlers,
  SpaceDeletedEvent,
} from "@habitat-network/jetstream";
import type { Effect } from "effect";

// A Domain is what one part of the app (files, apps, rosters, docs) indexes
// from the stream: handlers for the collections it owns and what to do when
// one of its spaces is deleted. appIndexer (./appIndexer) registers each
// Domain with the one LexIndexer, for the space types it lives in.
//
// Delivery is at-least-once, so every handler is idempotent: keyed on the
// event's uri, never on arrival order.
export interface Domain {
  readonly collections: ReadonlyArray<{
    readonly collection: string;
    readonly handlers: CommitHandlers<LexMap, unknown>;
  }>;
  readonly spaceDeleted: (
    event: SpaceDeletedEvent,
  ) => Effect.Effect<void, unknown>;
}
