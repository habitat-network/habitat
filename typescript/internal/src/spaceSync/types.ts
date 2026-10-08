import type { Cid, LexMap } from "@atproto/lex";
import type { VerifiedRecord } from "@atproto/space";
import type { Stream } from "effect";
import type { RepoVerificationError } from "./errors";

/** `at://{authority}/space/{type}/{skey}` */
export type SpaceRef = string;
export type Did = string;

/** Durable per-space sync state, owned by the host's SyncStore. Times are epoch ms. */
export interface SpaceState {
  readonly space: SpaceRef;
  readonly authority: Did;
  /** Last listRepos checkpoint every repo up to which has been applied. */
  readonly spaceRev?: string | undefined;
  readonly registrationExpiresAt?: number | undefined;
  readonly nextDueAt: number;
  readonly lastFullPassAt?: number | undefined;
  readonly failures: number;
  readonly lastError?: string | undefined;
}

/** Durable per-repo sync state: the verified revision and its LtHash state. */
export interface RepoState {
  readonly space: SpaceRef;
  readonly did: Did;
  readonly rev: string;
  /** 2048-byte LtHash state (`RepoCommit.setHash.state()`). */
  readonly ltHash: Uint8Array;
}

/** One record change. `cid: null` is a delete. `value` is absent when the host did not inline it. */
export interface Change {
  readonly uri: string;
  readonly collection: string;
  readonly rkey: string;
  readonly cid: Cid | null;
  readonly value?: LexMap | undefined;
}

export type RepoBatch =
  | {
      readonly _tag: "Ops";
      readonly space: SpaceRef;
      readonly did: Did;
      readonly rev: string;
      readonly changes: ReadonlyArray<Change>;
    }
  | {
      /** Replace the repo's contents with exactly these records. The sink must drain `records`. */
      readonly _tag: "Reset";
      readonly space: SpaceRef;
      readonly did: Did;
      readonly rev: string;
      readonly records: Stream.Stream<VerifiedRecord, RepoVerificationError>;
    }
  | {
      readonly _tag: "RepoRemoved";
      readonly space: SpaceRef;
      readonly did: Did;
    }
  | { readonly _tag: "SpaceDeleted"; readonly space: SpaceRef };

/** What SpaceSyncer.events publishes after a batch is committed. */
export type SyncEvent =
  | Extract<RepoBatch, { _tag: "Ops" | "RepoRemoved" | "SpaceDeleted" }>
  | {
      readonly _tag: "Reset";
      readonly space: SpaceRef;
      readonly did: Did;
      readonly rev: string;
    };

/** A writer as reported by listRepos. */
export interface ListedRepo {
  readonly did: Did;
  readonly repoRev: string;
  readonly spaceRev: string;
}
