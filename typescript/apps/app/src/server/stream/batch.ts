import type { Cid, LexMap } from "@atproto/lex";
import type { DidString, SpaceRefString } from "@atproto/syntax";

// One record change. cid is null for a delete.
export interface Change {
  readonly uri: string;
  readonly collection: string;
  readonly rkey: string;
  readonly cid: Cid | null;
  readonly value?: LexMap | undefined;
}

// A RepoBatch is what a stream event becomes for the sinks (see batchOf in
// ./indexer): a repo's record changes, a repo leaving a space, or the space
// being deleted. The stream replays from a cursor, so every sink applies a
// batch idempotently.
export type RepoBatch =
  | {
      readonly _tag: "Ops";
      readonly space: SpaceRefString;
      readonly did: DidString;
      readonly rev: string;
      readonly changes: ReadonlyArray<Change>;
    }
  | {
      readonly _tag: "RepoRemoved";
      readonly space: SpaceRefString;
      readonly did: DidString;
    }
  | { readonly _tag: "SpaceDeleted"; readonly space: SpaceRefString };

// A SyncSink applies batches to the app's D1 mirror. It must be idempotent
// and roll back if signal aborts.
export interface SyncSink {
  apply(batch: RepoBatch, signal: AbortSignal): Promise<void>;
}
