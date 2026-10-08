/// <reference lib="esnext.disposable" />
import { type Cid, parseCid } from "@atproto/lex";
import {
  RepoCommit,
  type SignedCommit,
  type VerifiedRecord,
  serializeRecord,
  verifyCommit,
  verifyRepoCar,
} from "@atproto/space";
import type {
  DidString,
  NsidString,
  RecordKeyString,
  SpaceRef,
  SpaceRefString,
} from "@atproto/syntax";
import type { com } from "api";
import { Effect, Option, Stream } from "effect";
import { SpaceSyncConfig } from "./config";
import {
  RepoSyncError,
  RepoUnavailableError,
  RepoVerificationError,
  type XrpcError,
  errorMessage,
} from "./errors";
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
import { REPO_UNAVAILABLE_CODES } from "./wire";

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

/**
 * Run a signature check with the DID's cached key, and once more with a freshly
 * resolved key if that fails and the key changed (rotation).
 */
const checkWithFreshKey = (
  space: SpaceRefString,
  did: DidString,
  check: (signingKey: string) => Promise<boolean>,
) =>
  Effect.gen(function* () {
    const identity = yield* Identity;
    const run = (key: string) =>
      Effect.promise(() => check(key).catch(() => false));
    const { signingKey } = yield* identity.resolve(did);
    if (yield* run(signingKey)) return true;
    const fresh = yield* identity.refresh(did);
    return fresh.signingKey !== signingKey && (yield* run(fresh.signingKey));
  }).pipe(
    Effect.mapError(
      (e) => new RepoVerificationError({ space, did, message: e.message }),
    ),
  );

/** A repo-host error: the repo being gone is terminal, anything else is retried later. */
const repoCallFailed = (
  space: SpaceRefString,
  did: DidString,
  error: XrpcError,
) =>
  error.error !== undefined && REPO_UNAVAILABLE_CODES.has(error.error)
    ? new RepoUnavailableError({
        space,
        did,
        code: error.error,
        message: error.message,
      })
    : new RepoSyncError({
        space,
        did,
        message: `${error.method} failed: ${error.message}`,
        cause: error,
      });

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
  ref: SpaceRef,
  listed: ListedRepo,
  local: RepoState,
) {
  const space = ref.toString();
  const client = yield* SpaceClient;
  const config = yield* SpaceSyncConfig;
  const did = listed.did;
  const verificationError = (message: string) =>
    new RepoVerificationError({ space, did, message });
  const state = RepoCommit.fromState(local.ltHash);
  // Later ops on a path supersede earlier ones, so the batch holds one change per path.
  const changes = new Map<string, Change>();
  const seenCursors = new Set<string>();
  let opCount = 0;
  let cursor: string | undefined;
  let commit: com.atproto.space.defs.SignedCommit | undefined;
  do {
    const page = yield* client.listRepoOps(ref, did, local.rev, cursor);
    opCount += page.ops.length;
    // Bounds memory and stops a host from paging forever; getRepo is the fallback.
    if (opCount > config.maxIncrementalOps)
      return yield* verificationError(
        `oplog exceeds ${config.maxIncrementalOps} ops`,
      );
    for (const op of page.ops) {
      if (op.rev <= local.rev)
        return yield* verificationError(
          `op ${op.rev} is not after ${local.rev}`,
        );
      const cid: Cid | null =
        op.cid === null ? null : yield* parseCidOr(space, did, op.cid);
      const prev: Cid | null =
        op.prev === null ? null : yield* parseCidOr(space, did, op.prev);
      yield* Effect.try({
        try: () =>
          state.applyOp({
            collection: op.collection,
            rkey: op.rkey,
            cid,
            prev,
          }),
        catch: (error) =>
          verificationError(`cannot apply op: ${errorMessage(error)}`),
      });
      const path = `${op.collection}/${op.rkey}`;
      // Re-insert so the change keeps the position of the path's latest op.
      changes.delete(path);
      changes.set(path, {
        uri: `${space}/${did}/${path}`,
        collection: op.collection,
        rkey: op.rkey,
        cid,
        value: cid ? op.value : undefined,
      });
    }
    commit = page.commit ?? commit;
    if (page.cursor !== undefined) {
      if (seenCursors.has(page.cursor))
        return yield* verificationError("listRepoOps cursor repeated");
      seenCursors.add(page.cursor);
    }
    cursor = page.cursor;
  } while (cursor);

  if (!commit)
    return yield* verificationError("oplog did not end with a commit");
  // The lexicon types `ver` as any integer; only version 1 is verifiable.
  if (commit.ver !== 1)
    return yield* verificationError(`unsupported commit version ${commit.ver}`);
  const finalCommit: SignedCommit = { ...commit, ver: 1 };
  const valid = yield* checkWithFreshKey(space, did, (key) =>
    verifyCommit(
      finalCommit,
      { space, author: did, rev: finalCommit.rev },
      key,
    ),
  );
  if (!valid)
    return yield* verificationError("commit signature or MAC is invalid");
  if (!state.matches(finalCommit))
    return yield* verificationError("set hash does not match commit");
  if (finalCommit.rev < listed.repoRev)
    return yield* verificationError("repo host is behind the listed revision");
  if (finalCommit.rev < local.rev)
    return yield* verificationError("repo host served an older commit");

  // The commit covers CIDs, not inlined values: each value must hash to its CID.
  for (const change of changes.values()) {
    const value = change.value;
    if (!change.cid || !value) continue;
    const { cid } = yield* Effect.tryPromise({
      try: () =>
        serializeRecord(
          change.collection as NsidString,
          change.rkey as RecordKeyString,
          value,
        ),
      catch: (error) =>
        verificationError(
          `cannot encode ${change.uri}: ${errorMessage(error)}`,
        ),
    });
    if (cid.toString() !== change.cid.toString())
      return yield* verificationError(
        `value of ${change.uri} does not match its cid`,
      );
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
  ref: SpaceRef,
  listed: ListedRepo,
  localRev: string | undefined,
) {
  const space = ref.toString();
  const client = yield* SpaceClient;
  const identity = yield* Identity;
  const did = listed.did;
  const syncError = (message: string, cause?: unknown) =>
    new RepoSyncError({ space, did, message, cause });

  const download = (signingKey: string) =>
    Effect.scoped(
      Effect.gen(function* () {
        const car = yield* client
          .getRepo(ref, did)
          .pipe(
            Effect.catchTag("XrpcError", (e) =>
              Effect.fail(repoCallFailed(space, did, e)),
            ),
          );
        // Verifies the commit and that the index matches its hash; records are checked as they stream.
        const verified = yield* Effect.acquireRelease(
          Effect.tryPromise({
            try: () =>
              verifyRepoCar(car, { space, author: did, didKey: signingKey }),
            catch: (error) =>
              new RepoVerificationError({
                space,
                did,
                message: `repo CAR failed verification: ${errorMessage(error)}`,
              }),
          }),
          (repo) => Effect.promise(() => repo[Symbol.asyncDispose]()),
        );
        const rev = verified.commit.rev;
        if (rev < listed.repoRev)
          return yield* syncError(
            `repo host is behind (${rev} < ${listed.repoRev})`,
          );
        // A validly signed but older snapshot would roll the sink back.
        if (localRev !== undefined && rev < localRev)
          return yield* syncError(
            `repo host served an older snapshot (${rev} < ${localRev})`,
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

  const { signingKey } = yield* identity
    .resolve(did)
    .pipe(Effect.mapError((e) => syncError(e.message, e)));
  return yield* download(signingKey).pipe(
    // The key may have rotated: retry once with a fresh one if it changed.
    Effect.catchTag("RepoVerificationError", (first) =>
      identity.refresh(did).pipe(
        Effect.mapError((e) => syncError(e.message, e)),
        Effect.flatMap((fresh) =>
          fresh.signingKey === signingKey
            ? Effect.fail(syncError(first.message, first))
            : download(fresh.signingKey).pipe(
                Effect.catchTag("RepoVerificationError", (e) =>
                  Effect.fail(syncError(e.message, e)),
                ),
              ),
        ),
      ),
    ),
  );
});

/**
 * Bring one repo up to `listed.repoRev`: incrementally via listRepoOps when local
 * state exists, falling back to a full getRepo when the oplog can't be verified
 * or the host rejects the request. Transient failures and an unavailable repo
 * go back to the caller instead of paying for a full download.
 */
export const syncRepo = Effect.fn("syncRepo")(function* (
  ref: SpaceRef,
  listed: ListedRepo,
) {
  const space = ref.toString();
  const store = yield* SyncStore;
  const local = yield* store.getRepo(space, listed.did);
  if (Option.isSome(local)) {
    const recovering = (message: string) =>
      Effect.logWarning("incremental sync failed; recovering", message).pipe(
        Effect.annotateLogs({ space, did: listed.did }),
        Effect.as(Option.none<Option.Option<SyncEvent>>()),
      );
    const result = yield* incremental(ref, listed, local.value).pipe(
      Effect.map(Option.some),
      Effect.catchTag("RepoVerificationError", (e) => recovering(e.message)),
      Effect.catchTag("XrpcError", (e) => {
        const failure = repoCallFailed(space, listed.did, e);
        return failure._tag === "RepoUnavailableError" || e.transient
          ? Effect.fail(failure)
          : recovering(e.message);
      }),
    );
    if (Option.isSome(result)) return result.value;
  }
  return yield* recover(
    ref,
    listed,
    Option.isSome(local) ? local.value.rev : undefined,
  );
});
