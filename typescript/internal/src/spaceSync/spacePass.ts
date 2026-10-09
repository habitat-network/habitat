import type { DidString, SpaceRef, SpaceRefString } from "@atproto/syntax";
import {
  Clock,
  Context,
  Duration,
  Effect,
  Layer,
  Option,
  Stream,
} from "effect";
import {
  type SpaceSyncOptions,
  SpaceSyncConfig,
  syncerServiceRef,
} from "./config";
import {
  type SinkError,
  type StoreError,
  XrpcError,
  errorMessage,
} from "./errors";
import { RepoSync } from "./repoSync";
import { type SpaceCallError, SpaceClient } from "./SpaceClient";
import { SyncSink } from "./SyncSink";
import { SyncStore } from "./SyncStore";
import type { ListedRepo, SyncEvent } from "./types";

export const backoffMillis = (
  config: SpaceSyncOptions,
  failures: number,
): number =>
  Math.min(
    Duration.toMillis(config.backoffBase) * 2 ** Math.max(0, failures - 1),
    Duration.toMillis(config.backoffCap),
  );

/**
 * Per-repo retry backoff, in memory. Without it every notification on a space
 * would re-download a repo that keeps failing (a cheap amplification for a bad
 * writer). Losing it on restart only costs one retry.
 */
export class RepoBackoff extends Context.Service<
  RepoBackoff,
  {
    /** When the repo may next be tried, if it is backing off. */
    readonly retryAt: (
      space: SpaceRefString,
      did: DidString,
    ) => Effect.Effect<number | undefined>;
    /** Record a failure now; returns when to retry. */
    readonly failed: (
      space: SpaceRefString,
      did: DidString,
    ) => Effect.Effect<number>;
    readonly clear: (
      space: SpaceRefString,
      did: DidString,
    ) => Effect.Effect<void>;
    readonly clearSpace: (space: SpaceRefString) => Effect.Effect<void>;
  }
>()("internal/spaceSync/RepoBackoff") {
  static readonly layer = Layer.effect(
    RepoBackoff,
    Effect.gen(function* () {
      const config = yield* SpaceSyncConfig;
      const entries = new Map<
        string,
        { readonly failures: number; readonly retryAt: number }
      >();
      const key = (space: SpaceRefString, did: DidString) => `${space} ${did}`;
      return RepoBackoff.of({
        retryAt: (space, did) =>
          Effect.sync(() => entries.get(key(space, did))?.retryAt),
        failed: (space, did) =>
          Effect.map(Clock.currentTimeMillis, (now) => {
            const failures = (entries.get(key(space, did))?.failures ?? 0) + 1;
            const retryAt = now + backoffMillis(config, failures);
            entries.set(key(space, did), { failures, retryAt });
            return retryAt;
          }),
        clear: (space, did) =>
          Effect.sync(() => void entries.delete(key(space, did))),
        clearSpace: (space) =>
          Effect.sync(() => {
            for (const k of entries.keys())
              if (k.startsWith(`${space} `)) entries.delete(k);
          }),
      });
    }),
  );
}

const LIST_REPOS = "com.atproto.space.listRepos";

const invalidListing = (message: string) =>
  new XrpcError({
    method: LIST_REPOS,
    status: 200,
    error: "InvalidResponse",
    message,
    transient: false,
  });

type RepoOutcome =
  | { readonly kind: "ok"; readonly event: Option.Option<SyncEvent> }
  /** Gone at its host; doesn't hold back the checkpoint, retried on the next full pass. */
  | { readonly kind: "unavailable" }
  /** Failed now, or skipped while backing off; holds back the checkpoint. */
  | {
      readonly kind: "failed" | "deferred";
      readonly retryAt: number;
      readonly error: string;
    };

export type SpacePassError = SpaceCallError | StoreError | SinkError;

/** Reconcile passes over a space: walk the writer set, sync stale repos, checkpoint. */
export class SpacePass extends Context.Service<
  SpacePass,
  {
    /**
     * One reconcile pass over a space. Callers (SpaceSyncer) guarantee passes for
     * a space never overlap. `full` walks the whole writer set (and prunes removed
     * writers); otherwise the walk resumes from the stored checkpoint unless a full
     * pass is due anyway.
     */
    readonly run: (
      space: SpaceRef,
      full: boolean,
    ) => Effect.Effect<ReadonlyArray<SyncEvent>, SpacePassError>;
    /** Record a failed pass on the space and schedule a backed-off retry. Never fails. */
    readonly recordFailure: (
      space: SpaceRefString,
      error: unknown,
    ) => Effect.Effect<void>;
  }
>()("internal/spaceSync/SpacePass") {
  static readonly layer = Layer.effect(
    SpacePass,
    Effect.gen(function* () {
      const config = yield* SpaceSyncConfig;
      const store = yield* SyncStore;
      const client = yield* SpaceClient;
      const sink = yield* SyncSink;
      const backoff = yield* RepoBackoff;
      const repoSync = yield* RepoSync;
      const renewLeadMs = Duration.toMillis(config.registrationRenewLead);
      const fullIntervalMs = Duration.toMillis(config.fullPassInterval);

      /**
       * The writer set after `from`, one element per listed repo. Entries must be
       * strictly ascending by spaceRev and each non-empty page's cursor must equal
       * its last entry.
       */
      const listWriters = (ref: SpaceRef, from: string | undefined) =>
        Stream.paginate(from, (cursor) =>
          client.listRepos(ref, cursor).pipe(
            Effect.flatMap((page) => {
              let last = cursor;
              for (const repo of page.repos) {
                if (last !== undefined && repo.spaceRev <= last)
                  return Effect.fail(
                    invalidListing(
                      "listRepos entries are not in ascending spaceRev order",
                    ),
                  );
                last = repo.spaceRev;
              }
              if (page.repos.length > 0 && page.cursor !== last)
                return Effect.fail(
                  invalidListing(
                    "listRepos cursor does not match its last entry",
                  ),
                );
              return Effect.succeed([
                page.repos,
                page.repos.length === 0 ? Option.none() : Option.some(last),
              ] as const);
            }),
          ),
        );

      /** Sync one listed repo, turning per-repo failures into outcomes. */
      const syncListed = (ref: SpaceRef, repo: ListedRepo, now: number) => {
        const space = ref.toString();
        return Effect.gen(function* () {
          const local = yield* store.getRepo(space, repo.did);
          if (Option.isSome(local) && local.value.rev >= repo.repoRev) {
            yield* backoff.clear(space, repo.did);
            return { kind: "ok", event: Option.none() } as const;
          }
          const retryAt = yield* backoff.retryAt(space, repo.did);
          if (retryAt !== undefined && retryAt > now)
            return {
              kind: "deferred",
              retryAt,
              error: "backing off after earlier failures",
            } as const;
          const event = yield* repoSync.sync(ref, repo);
          yield* backoff.clear(space, repo.did);
          return { kind: "ok", event } as const;
        }).pipe(
          Effect.catchTags({
            RepoUnavailableError: (error) =>
              Effect.logInfo("repo unavailable", error).pipe(
                Effect.andThen(backoff.clear(space, repo.did)),
                Effect.as({ kind: "unavailable" } as const),
              ),
            // Per-repo failures don't fail the pass; the checkpoint makes sure they're retried.
            RepoSyncError: (error) => failed(space, repo, error),
            SinkError: (error) => failed(space, repo, error),
          }),
          Effect.annotateLogs({ space, did: repo.did }),
        );
      };

      const failed = (
        space: SpaceRefString,
        repo: ListedRepo,
        error: { readonly message: string },
      ) =>
        Effect.logWarning("repo sync failed", error).pipe(
          Effect.andThen(backoff.failed(space, repo.did)),
          Effect.map(
            (retryAt) =>
              ({ kind: "failed", retryAt, error: error.message }) as const,
          ),
        );

      const run = Effect.fn("SpacePass.run")(function* (
        ref: SpaceRef,
        requestFull: boolean,
      ): Effect.fn.Return<ReadonlyArray<SyncEvent>, SpacePassError> {
        const space = ref.toString();
        const current = yield* store.getSpace(space);
        if (Option.isNone(current)) return [];
        let state = current.value;
        const now = yield* Clock.currentTimeMillis;

        // Best effort: a failed registration only delays notifications, so it must
        // not stop the pass. SpaceDeleted still propagates.
        let registrationFailed = false;
        if (
          config.serviceDid &&
          (state.registrationExpiresAt === undefined ||
            state.registrationExpiresAt - now <= renewLeadMs)
        ) {
          const registered = yield* client
            .registerNotify(ref, syncerServiceRef(config.serviceDid))
            .pipe(
              Effect.map(Option.some),
              Effect.catchIf(
                (error) =>
                  !(
                    error._tag === "CredentialError" &&
                    error.reason === "SpaceDeleted"
                  ),
                (error) =>
                  Effect.logWarning("registerNotify failed", error).pipe(
                    Effect.as(Option.none<{ readonly expiresAt: number }>()),
                  ),
              ),
            );
          if (Option.isSome(registered)) {
            state = {
              ...state,
              registrationExpiresAt: registered.value.expiresAt,
            };
            yield* store.putSpace(state);
          } else {
            registrationFailed = true;
          }
        }

        const full =
          requestFull ||
          state.spaceRev === undefined ||
          (state.lastFullPassAt ?? 0) + fullIntervalMs <= now;

        // A writer that reappears in the walk keeps only its newest listing.
        const from = full ? undefined : state.spaceRev;
        const listed = yield* Stream.runFold(
          listWriters(ref, from),
          () => new Map<DidString, ListedRepo>(),
          (acc, repo) =>
            acc.set(repo.did, {
              did: repo.did,
              repoRev: repo.repoRev,
              spaceRev: repo.spaceRev,
            }),
        );

        const pending = [...listed.values()].sort((a, b) =>
          a.spaceRev < b.spaceRev ? -1 : 1,
        );
        const outcomes: ReadonlyArray<RepoOutcome> = yield* Effect.forEach(
          pending,
          (repo) => syncListed(ref, repo, now),
          { concurrency: config.repoConcurrency },
        );

        const events: SyncEvent[] = outcomes.flatMap((o) =>
          o.kind === "ok" && Option.isSome(o.event) ? [o.event.value] : [],
        );

        if (full) {
          for (const did of yield* store.listRepoDids(space)) {
            if (listed.has(did)) continue;
            yield* Effect.uninterruptible(
              sink
                .apply({ _tag: "RepoRemoved", space, did })
                .pipe(Effect.andThen(store.removeRepo(space, did))),
            );
            yield* backoff.clear(space, did);
            events.push({ _tag: "RepoRemoved", space, did });
          }
        }

        // Never checkpoint past a repo that still has to be retried: resume from the last entry before it.
        const firstBlocked = outcomes.findIndex(
          (o) => o.kind === "failed" || o.kind === "deferred",
        );
        const newest = pending.at(-1)?.spaceRev ?? from;
        const checkpoint =
          firstBlocked === -1
            ? (newest ?? state.spaceRev)
            : firstBlocked === 0
              ? state.spaceRev
              : pending[firstBlocked - 1].spaceRev;
        const failedRepos = outcomes.flatMap((o, i) =>
          o.kind === "failed" ? [{ did: pending[i].did, error: o.error }] : [],
        );
        const failures =
          failedRepos.length > 0
            ? state.failures + 1
            : firstBlocked === -1
              ? 0
              : state.failures;
        const lastFullPassAt = full ? now : state.lastFullPassAt;
        const candidates = [(lastFullPassAt ?? now) + fullIntervalMs];
        if (state.registrationExpiresAt !== undefined)
          candidates.push(state.registrationExpiresAt - renewLeadMs);
        if (registrationFailed) candidates.push(now + backoffMillis(config, 1));
        for (const o of outcomes)
          if (o.kind === "failed" || o.kind === "deferred")
            candidates.push(o.retryAt);

        yield* store.putSpace({
          ...state,
          spaceRev: checkpoint,
          lastFullPassAt,
          failures,
          lastError:
            failedRepos.length > 0
              ? `${failedRepos[0].did}: ${failedRepos[0].error}`
              : undefined,
          nextDueAt: Math.min(...candidates),
        });
        return events;
      });

      const recordFailure = Effect.fnUntraced(
        function* (space: SpaceRefString, error: unknown) {
          yield* Effect.logWarning("space pass failed", error).pipe(
            Effect.annotateLogs({ space }),
          );
          const current = yield* store.getSpace(space);
          if (Option.isNone(current)) return;
          const now = yield* Clock.currentTimeMillis;
          const failures = current.value.failures + 1;
          yield* store.putSpace({
            ...current.value,
            failures,
            lastError: errorMessage(error),
            nextDueAt: now + backoffMillis(config, failures),
          });
        },
        Effect.catch((e) =>
          Effect.logError("could not record pass failure", e),
        ),
      );

      return SpacePass.of({ run, recordFailure });
    }),
  );
}
