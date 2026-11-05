/// <reference lib="esnext.disposable" />
import { type Cid, parseCid } from "@atproto/lex";
import {
  type Commit,
  RepoCommit,
  type VerifiedRecord,
  serializeRecord,
  verifyCommit,
  verifyRepoStar,
} from "@atproto/space";
import type {
  DidString,
  NsidString,
  RecordKeyString,
  SpaceRef,
  SpaceRefString,
} from "@atproto/syntax";
import type { com } from "api";
import { Context, Effect, Layer, Option, Stream } from "effect";
import { SpaceSyncConfig } from "./config";
import {
  type CredentialError,
  RepoSyncError,
  RepoUnavailableError,
  RepoVerificationError,
  type SinkError,
  type StoreError,
  type XrpcError,
  errorMessage,
} from "./errors";
import { type ResponseMac, SpaceClient } from "./SpaceClient";
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

/** Brings one repo in a space up to its listed revision, verifying everything it applies. */
export class RepoSync extends Context.Service<
  RepoSync,
  {
    /**
     * Bring one repo up to `listed.repoRev`: incrementally via listRepoOps when
     * local state exists, falling back to a full getRepo when the oplog can't be
     * verified or the host rejects the request. Transient failures and an
     * unavailable repo go back to the caller instead of paying for a full download.
     */
    readonly sync: (
      space: SpaceRef,
      listed: ListedRepo,
    ) => Effect.Effect<
      Option.Option<SyncEvent>,
      | RepoSyncError
      | RepoUnavailableError
      | SinkError
      | StoreError
      | CredentialError
    >;
  }
>()("internal/spaceSync/RepoSync") {
  static readonly layer = Layer.effect(
    RepoSync,
    Effect.gen(function* () {
      const config = yield* SpaceSyncConfig;
      const client = yield* SpaceClient;
      const store = yield* SyncStore;
      const sink = yield* SyncSink;

      /**
       * Deliver a batch, then persist the repo state. apply may be interrupted (the
       * sink rolls back); once it returns, putRepo always runs so sink and store stay
       * in step. A crash between the two replays the batch (at-least-once).
       * `state` is read once apply returns, so it can depend on records the sink
       * drained.
       */
      const commitBatch = (batch: RepoBatch, state: () => RepoState) =>
        Effect.uninterruptibleMask((restore) =>
          (batch._tag === "Ops" && batch.changes.length === 0
            ? Effect.void
            : restore(sink.apply(batch))
          ).pipe(Effect.andThen(Effect.suspend(() => store.putRepo(state())))),
        );

      const incremental = Effect.fnUntraced(function* (
        ref: SpaceRef,
        listed: ListedRepo,
        local: RepoState,
      ) {
        const space = ref.toString();
        const did = listed.did;
        const verificationError = (message: string) =>
          new RepoVerificationError({ space, did, message });
        const state = RepoCommit.fromState(local.ltHash);
        // Later ops on a path supersede earlier ones, so the batch holds one change per path.
        const changes = new Map<string, Change>();
        const seenCursors = new Set<string>();
        let opCount = 0;
        let commit: com.atproto.space.defs.Commit | undefined;
        let commitMac: ResponseMac | undefined;

        // One element per listRepoOps page; a repeated cursor would page forever.
        const pages = Stream.paginate(
          undefined as string | undefined,
          (cursor) =>
            client.listRepoOps(ref, did, local.rev, cursor).pipe(
              Effect.flatMap((response) => {
                const cursor = response.body.cursor;
                if (cursor !== undefined) {
                  if (seenCursors.has(cursor))
                    return Effect.fail(
                      verificationError("listRepoOps cursor repeated"),
                    );
                  seenCursors.add(cursor);
                }
                return Effect.succeed([
                  [response],
                  Option.fromNullishOr(cursor),
                ] as const);
              }),
            ),
        );
        yield* Stream.runForEach(pages, (response) =>
          Effect.gen(function* () {
            const { body: page, mac } = response;
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
                op.prev === null
                  ? null
                  : yield* parseCidOr(space, did, op.prev);
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
            if (page.commit) {
              commit = page.commit;
              commitMac = mac;
            }
          }),
        );

        if (!commit)
          return yield* verificationError("oplog did not end with a commit");
        // The lexicon types `ver` as any integer; only version 1 is verifiable.
        if (commit.ver !== 1)
          return yield* verificationError(
            `unsupported commit version ${commit.ver}`,
          );
        const finalCommit = { ...commit, ver: 1 } as Commit;
        // The commit is authenticated by its page's MAC, under the secret
        // negotiated for that one request.
        const valid =
          commitMac?.mac !== undefined &&
          verifyCommit(finalCommit, {
            space,
            repo: did,
            sharedSecret: commitMac.sharedSecret,
            mac: commitMac.mac,
          });
        if (!valid) {
          // The KEM key may have rotated: re-resolve it so the getRepo this
          // falls back to negotiates against the current one.
          yield* client
            .refreshKem(ref, did)
            .pipe(Effect.catchTag("XrpcError", () => Effect.void));
          return yield* verificationError("commit MAC is missing or invalid");
        }
        if (!state.matches(finalCommit))
          return yield* verificationError("set hash does not match commit");
        if (finalCommit.rev < listed.repoRev)
          return yield* verificationError(
            "repo host is behind the listed revision",
          );
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
        yield* commitBatch(batch, () => ({
          space,
          did,
          rev: finalCommit.rev,
          ltHash: state.setHash.state(),
        }));
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
        const did = listed.did;
        const syncError = (message: string, cause?: unknown) =>
          new RepoSyncError({ space, did, message, cause });

        const download = Effect.fnUntraced(function* () {
          const { star, mac } = yield* client
            .getRepo(ref, did)
            .pipe(
              Effect.catchTag("XrpcError", (e) =>
                Effect.fail(repoCallFailed(space, did, e)),
              ),
            );
          if (!mac.mac)
            return yield* new RepoVerificationError({
              space,
              did,
              message: "repo STAR response carried no MAC",
            });
          const responseMac = mac.mac;
          // Authenticates the commit by the response's MAC; records are folded
          // into a set hash as they stream and checked against the commit's
          // data once the archive ends.
          const verified = yield* Effect.acquireRelease(
            Effect.tryPromise({
              try: () =>
                verifyRepoStar(star, {
                  space,
                  repo: did,
                  sharedSecret: mac.sharedSecret,
                  mac: responseMac,
                }),
              catch: (error) =>
                new RepoVerificationError({
                  space,
                  did,
                  message: `repo STAR failed verification: ${errorMessage(error)}`,
                }),
            }),
            (repo) => Effect.promise(() => repo[Symbol.asyncDispose]()),
          );
          const rev = verified.commit.rev;
          if (rev < listed.repoRev)
            return yield* syncError(
              `repo host is behind (${rev} < ${listed.repoRev})`,
            );
          // An authentic but older snapshot would roll the sink back.
          if (localRev !== undefined && rev < localRev)
            return yield* syncError(
              `repo host served an older snapshot (${rev} < ${localRev})`,
            );
          // The archive carries no set hash state, so rebuild it from the records as
          // the sink drains them; the archive fails the stream if they don't match
          // the commit.
          const setHash = new RepoCommit();
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
          ).pipe(
            Stream.tap((record) =>
              Effect.sync(() =>
                setHash.add(record.collection, record.rkey, record.cid),
              ),
            ),
          );
          yield* commitBatch(
            { _tag: "Reset", space, did, rev, records },
            () => ({
              space,
              did,
              rev,
              ltHash: setHash.setHash.state(),
            }),
          );
          return Option.some<SyncEvent>({ _tag: "Reset", space, did, rev });
        }, Effect.scoped);

        return yield* download().pipe(
          // The KEM key may have rotated: retry once if a fresh resolution
          // changed it.
          Effect.catchTag("RepoVerificationError", (first) =>
            client.refreshKem(ref, did).pipe(
              Effect.catchTag("XrpcError", (e) =>
                Effect.fail(syncError(e.message, e)),
              ),
              Effect.flatMap((changed) =>
                changed
                  ? download().pipe(
                      Effect.catchTag("RepoVerificationError", (e) =>
                        Effect.fail(syncError(e.message, e)),
                      ),
                    )
                  : Effect.fail(syncError(first.message, first)),
              ),
            ),
          ),
        );
      });

      const sync = Effect.fn("RepoSync.sync")(function* (
        ref: SpaceRef,
        listed: ListedRepo,
      ) {
        const space = ref.toString();
        const local = yield* store.getRepo(space, listed.did);
        if (Option.isSome(local)) {
          const recovering = (error: RepoVerificationError | XrpcError) =>
            Effect.logWarning(
              "incremental sync failed; recovering",
              error,
            ).pipe(
              Effect.annotateLogs({ space, did: listed.did }),
              Effect.as(Option.none<Option.Option<SyncEvent>>()),
            );
          const result = yield* incremental(ref, listed, local.value).pipe(
            Effect.map(Option.some),
            Effect.catchTags({
              RepoVerificationError: recovering,
              XrpcError: (e) => {
                const failure = repoCallFailed(space, listed.did, e);
                return failure._tag === "RepoUnavailableError" || e.transient
                  ? Effect.fail(failure)
                  : recovering(e);
              },
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

      return RepoSync.of({ sync });
    }),
  );
}
