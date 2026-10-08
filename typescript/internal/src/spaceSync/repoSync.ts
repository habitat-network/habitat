import { type Cid, type LexMap, parseCid } from "@atproto/lex";
import {
  RepoCommit,
  type SignedCommit,
  type VerifiedRecord,
  verifyCommit,
  verifyRepoCar,
} from "@atproto/space";
import type {
  DidString,
  NsidString,
  RecordKeyString,
  SpaceRefString,
} from "@atproto/syntax";
import type { com } from "api";
import { Effect, Option, Predicate, Stream } from "effect";
import { RepoSyncError, RepoVerificationError, errorMessage } from "./errors";
import { Identity } from "./Identity";
import { SpaceClient } from "./SpaceClient";
import { SyncSink } from "./SyncSink";
import { SyncStore } from "./SyncStore";
import type {
  Change,
  ListedRepo,
  RepoBatch,
  RepoState,
  SyncEvent,
} from "./types";

/**
 * `Symbol.asyncDispose` is ESNext and this repo's `lib` stops at ES2024, so reach
 * for the well-known symbol explicitly rather than widening the whole repo's lib.
 */
const asyncDisposeSymbol = (
  Symbol as unknown as { readonly asyncDispose: symbol }
).asyncDispose;
const dispose = <A>(repo: A) =>
  Effect.promise(() =>
    (repo as unknown as Record<symbol, () => Promise<void>>)[
      asyncDisposeSymbol
    ](),
  );

/**
 * Deliver a batch, then persist the repo state. apply may be interrupted (the
 * sink rolls back); once it returns, putRepo always runs so sink and store stay
 * in step. A crash between the two replays the batch (at-least-once).
 */
const commitBatch = (batch: RepoBatch, state: RepoState) =>
  Effect.uninterruptibleMask((restore) =>
    Effect.gen(function* () {
      const sink = yield* SyncSink;
      const store = yield* SyncStore;
      if (!(batch._tag === "Ops" && batch.changes.length === 0))
        yield* restore(sink.apply(batch));
      yield* store.putRepo(state);
    }),
  );

const parseCidOr = (space: SpaceRefString, did: DidString, value: string) =>
  Effect.try({
    try: () => parseCid(value),
    catch: () =>
      new RepoVerificationError({
        space,
        did,
        message: `invalid cid ${value}`,
      }),
  });

const incremental = Effect.fnUntraced(function* (
  space: SpaceRefString,
  listed: ListedRepo,
  local: RepoState,
) {
  const client = yield* SpaceClient;
  const identity = yield* Identity;
  const did = listed.did;
  const state = RepoCommit.fromState(local.ltHash);
  // Later ops on a path supersede earlier ones, so the batch holds one change per path.
  const changes = new Map<string, Change>();
  let cursor: string | undefined;
  let commit: com.atproto.space.defs.SignedCommit | undefined;
  do {
    const page = yield* client.listRepoOps(space, did, local.rev, cursor);
    for (const op of page.ops) {
      const cid: Cid | null =
        op.cid === null ? null : yield* parseCidOr(space, did, op.cid);
      const prev: Cid | null =
        op.prev === null ? null : yield* parseCidOr(space, did, op.prev);
      yield* Effect.try({
        try: () =>
          state.applyOp({
            collection: op.collection as NsidString,
            rkey: op.rkey as RecordKeyString,
            cid,
            prev,
          }),
        catch: (error) =>
          new RepoVerificationError({
            space,
            did,
            message: `cannot apply op: ${errorMessage(error)}`,
          }),
      });
      const path = `${op.collection}/${op.rkey}`;
      changes.delete(path);
      changes.set(path, {
        uri: `${space}/${did}/${path}`,
        collection: op.collection,
        rkey: op.rkey,
        cid,
        value:
          cid && Predicate.isObject(op.value)
            ? (op.value as LexMap)
            : undefined,
      });
    }
    commit = page.commit ?? commit;
    cursor = page.cursor;
  } while (cursor);

  if (!commit)
    return yield* new RepoVerificationError({
      space,
      did,
      message: "oplog did not end with a commit",
    });
  // The lexicon types `ver` as any integer; only version 1 is verifiable.
  if (commit.ver !== 1)
    return yield* new RepoVerificationError({
      space,
      did,
      message: `unsupported commit version ${commit.ver}`,
    });
  const finalCommit: SignedCommit = { ...commit, ver: 1 };
  const { signingKey } = yield* identity
    .resolve(did)
    .pipe(
      Effect.mapError(
        (e) => new RepoVerificationError({ space, did, message: e.message }),
      ),
    );
  const valid = yield* Effect.promise(() =>
    verifyCommit(
      finalCommit,
      { space, author: did, rev: finalCommit.rev },
      signingKey,
    ).catch(() => false),
  );
  if (!valid)
    return yield* new RepoVerificationError({
      space,
      did,
      message: "commit signature or MAC is invalid",
    });
  if (!state.matches(finalCommit))
    return yield* new RepoVerificationError({
      space,
      did,
      message: "set hash does not match commit",
    });
  if (finalCommit.rev < listed.repoRev) {
    return yield* new RepoVerificationError({
      space,
      did,
      message: "repo host is behind the listed revision",
    });
  }
  const batch = {
    _tag: "Ops" as const,
    space,
    did,
    rev: finalCommit.rev,
    changes: [...changes.values()],
  };
  yield* commitBatch(batch, {
    space,
    did,
    rev: finalCommit.rev,
    ltHash: state.setHash.state(),
  });
  return batch.changes.length === 0
    ? Option.none<SyncEvent>()
    : Option.some<SyncEvent>(batch);
});

const recover = Effect.fnUntraced(function* (
  space: SpaceRefString,
  listed: ListedRepo,
) {
  const client = yield* SpaceClient;
  const identity = yield* Identity;
  const did = listed.did;
  const syncError = (message: string, cause?: unknown) =>
    new RepoSyncError({ space, did, message, cause });
  const { signingKey } = yield* identity
    .resolve(did)
    .pipe(Effect.mapError((e) => syncError(e.message, e)));

  return yield* Effect.scoped(
    Effect.gen(function* () {
      const car = yield* client
        .getRepo(space, did)
        .pipe(
          Effect.catchTag("XrpcError", (e) =>
            Effect.fail(syncError(`getRepo failed: ${e.message}`, e)),
          ),
        );
      // Verifies the commit and that the index matches its hash; records are checked as they stream.
      const verified = yield* Effect.acquireRelease(
        Effect.tryPromise({
          try: () =>
            verifyRepoCar(car, { space, author: did, didKey: signingKey }),
          catch: (error) =>
            syncError(
              `repo CAR failed verification: ${errorMessage(error)}`,
              error,
            ),
        }),
        dispose,
      );
      const rev = verified.commit.rev;
      if (rev < listed.repoRev)
        return yield* syncError(
          `repo host is behind (${rev} < ${listed.repoRev})`,
        );
      const records = Stream.fromAsyncIterable<
        VerifiedRecord,
        RepoVerificationError
      >(
        verified.records,
        (error) =>
          new RepoVerificationError({
            space,
            did,
            message: errorMessage(error),
          }),
      );
      yield* commitBatch(
        { _tag: "Reset", space, did, rev, records },
        { space, did, rev, ltHash: verified.repo.setHash.state() },
      );
      return Option.some<SyncEvent>({ _tag: "Reset", space, did, rev });
    }),
  );
});

/**
 * Bring one repo up to `listed.repoRev`: incrementally via listRepoOps when local
 * state exists, otherwise (or on any verification failure) via a full getRepo.
 */
export const syncRepo = Effect.fn("syncRepo")(function* (
  space: SpaceRefString,
  listed: ListedRepo,
) {
  const store = yield* SyncStore;
  const local = yield* store.getRepo(space, listed.did);
  if (Option.isSome(local)) {
    const result = yield* incremental(space, listed, local.value).pipe(
      Effect.map(Option.some),
      Effect.catchTag(["RepoVerificationError", "XrpcError"], (error) =>
        Effect.logWarning(
          "incremental sync failed; recovering",
          error.message,
        ).pipe(
          Effect.annotateLogs({ space, did: listed.did }),
          Effect.as(Option.none<Option.Option<SyncEvent>>()),
        ),
      ),
    );
    if (Option.isSome(result)) return result.value;
  }
  return yield* recover(space, listed);
});
