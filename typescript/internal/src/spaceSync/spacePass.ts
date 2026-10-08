import type { DidString, SpaceRef, SpaceRefString } from "@atproto/syntax";
import { Clock, Context, Duration, Effect, Layer, Option } from "effect";
import { type SpaceSyncOptions, SpaceSyncConfig } from "./config";
import { XrpcError, errorMessage } from "./errors";
import { syncRepo } from "./repoSync";
import { SpaceClient } from "./SpaceClient";
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

/**
 * One reconcile pass over a space. Callers (SpaceSyncer) guarantee passes for a
 * space never overlap. `full` walks the whole writer set (and prunes removed
 * writers); otherwise the walk resumes from the stored checkpoint unless a full
 * pass is due anyway.
 */
export const runSpacePass = Effect.fn("runSpacePass")(function* (
  ref: SpaceRef,
  requestFull: boolean,
) {
  const space = ref.toString();
  const store = yield* SyncStore;
  const client = yield* SpaceClient;
  const sink = yield* SyncSink;
  const backoff = yield* RepoBackoff;
  const config = yield* SpaceSyncConfig;

  const current = yield* store.getSpace(space);
  if (Option.isNone(current)) return [] as ReadonlyArray<SyncEvent>;
  let state = current.value;
  const now = yield* Clock.currentTimeMillis;
  const renewLeadMs = Duration.toMillis(config.registrationRenewLead);
  const fullIntervalMs = Duration.toMillis(config.fullPassInterval);

  // Best effort: a failed registration only delays notifications, so it must not
  // stop the pass. SpaceDeleted still propagates.
  let registrationFailed = false;
  if (
    config.serviceDid &&
    (state.registrationExpiresAt === undefined ||
      state.registrationExpiresAt - now <= renewLeadMs)
  ) {
    const registered = yield* client
      .registerNotify(ref, config.serviceDid)
      .pipe(
        Effect.map(Option.some),
        Effect.catch((error) =>
          error._tag === "CredentialError" && error.reason === "SpaceDeleted"
            ? Effect.fail(error)
            : Effect.logWarning("registerNotify failed", error.message).pipe(
                Effect.annotateLogs({ space }),
                Effect.as(Option.none<{ readonly expiresAt: number }>()),
              ),
        ),
      );
    if (Option.isSome(registered)) {
      state = { ...state, registrationExpiresAt: registered.value.expiresAt };
      yield* store.putSpace(state);
    } else {
      registrationFailed = true;
    }
  }

  const full =
    requestFull ||
    state.spaceRev === undefined ||
    (state.lastFullPassAt ?? 0) + fullIntervalMs <= now;

  // Walk the writer set. Entries must be strictly ascending by spaceRev and each
  // non-empty page's cursor must equal its last entry; a writer that reappears
  // keeps only its newest listing.
  const listed = new Map<DidString, ListedRepo>();
  let cursor = full ? undefined : state.spaceRev;
  while (true) {
    const page = yield* client.listRepos(ref, cursor);
    if (page.repos.length === 0) break;
    for (const repo of page.repos) {
      if (cursor !== undefined && repo.spaceRev <= cursor) {
        return yield* new XrpcError({
          method: LIST_REPOS,
          status: 200,
          error: "InvalidResponse",
          message: "listRepos entries are not in ascending spaceRev order",
          transient: false,
        });
      }
      listed.set(repo.did, {
        did: repo.did,
        repoRev: repo.repoRev,
        spaceRev: repo.spaceRev,
      });
      cursor = repo.spaceRev;
    }
    if (page.cursor !== cursor) {
      return yield* new XrpcError({
        method: LIST_REPOS,
        status: 200,
        error: "InvalidResponse",
        message: "listRepos cursor does not match its last entry",
        transient: false,
      });
    }
  }

  const pending = [...listed.values()].sort((a, b) =>
    a.spaceRev < b.spaceRev ? -1 : 1,
  );
  const outcomes: ReadonlyArray<RepoOutcome> = yield* Effect.forEach(
    pending,
    (repo) =>
      Effect.gen(function* () {
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
        const event = yield* syncRepo(ref, repo);
        yield* backoff.clear(space, repo.did);
        return { kind: "ok", event } as const;
      }).pipe(
        Effect.catchTag("RepoUnavailableError", (error) =>
          Effect.logInfo(
            "repo unavailable",
            `${error.code}: ${error.message}`,
          ).pipe(
            Effect.andThen(backoff.clear(space, repo.did)),
            Effect.as({ kind: "unavailable" } as const),
          ),
        ),
        // Per-repo failures don't fail the pass; the checkpoint below makes sure they're retried.
        Effect.catchTag(["RepoSyncError", "SinkError"], (error) =>
          Effect.logWarning("repo sync failed", error.message).pipe(
            Effect.andThen(backoff.failed(space, repo.did)),
            Effect.map(
              (retryAt) =>
                ({ kind: "failed", retryAt, error: error.message }) as const,
            ),
          ),
        ),
        Effect.annotateLogs({ space, did: repo.did }),
      ),
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
  const checkpoint =
    firstBlocked === -1
      ? (cursor ?? state.spaceRev)
      : firstBlocked === 0
        ? state.spaceRev
        : pending[firstBlocked - 1].spaceRev;
  const failed = outcomes.flatMap((o, i) =>
    o.kind === "failed" ? [{ did: pending[i].did, error: o.error }] : [],
  );
  const failures =
    failed.length > 0
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
      failed.length > 0 ? `${failed[0].did}: ${failed[0].error}` : undefined,
    nextDueAt: Math.min(...candidates),
  });
  return events as ReadonlyArray<SyncEvent>;
});

/** Record a failed pass on the space and schedule a backed-off retry. Never fails. */
export const recordPassFailure = (space: SpaceRefString, error: unknown) =>
  Effect.gen(function* () {
    const store = yield* SyncStore;
    const config = yield* SpaceSyncConfig;
    yield* Effect.logWarning("space pass failed", errorMessage(error)).pipe(
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
  }).pipe(
    Effect.catch((e) =>
      Effect.logError("could not record pass failure", errorMessage(e)),
    ),
  );
