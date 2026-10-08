import { Clock, Duration, Effect, Option } from "effect";
import { type SpaceSyncOptions, SpaceSyncConfig } from "./config";
import { XrpcError, errorMessage } from "./errors";
import { syncRepo } from "./repoSync";
import { SpaceClient } from "./SpaceClient";
import { SyncSink } from "./SyncSink";
import { SyncStore } from "./SyncStore";
import type { ListedRepo, SyncEvent } from "./types";

export type PassKind = "CatchUp" | "Maintenance" | "Full";

export const backoffMillis = (config: SpaceSyncOptions, failures: number): number =>
  Math.min(
    Duration.toMillis(config.backoffBase) * 2 ** Math.max(0, failures - 1),
    Duration.toMillis(config.backoffCap),
  );

const LIST_REPOS = "com.atproto.space.listRepos";

/**
 * One reconcile pass over a space. Callers (SpaceSyncer) guarantee passes for a
 * space never overlap.
 */
export const runSpacePass = Effect.fn("runSpacePass")(function* (space: string, kind: PassKind) {
  const store = yield* SyncStore;
  const client = yield* SpaceClient;
  const sink = yield* SyncSink;
  const config = yield* SpaceSyncConfig;

  const current = yield* store.getSpace(space);
  if (Option.isNone(current)) return [] as ReadonlyArray<SyncEvent>;
  let state = current.value;
  const now = yield* Clock.currentTimeMillis;
  const renewLeadMs = Duration.toMillis(config.registrationRenewLead);
  const fullIntervalMs = Duration.toMillis(config.fullPassInterval);

  if (config.serviceDid && (state.registrationExpiresAt === undefined || state.registrationExpiresAt - now <= renewLeadMs)) {
    const { expiresAt } = yield* client.registerNotify(space, config.serviceDid);
    state = { ...state, registrationExpiresAt: expiresAt };
    yield* store.putSpace(state);
  }

  const full =
    kind === "Full" || state.spaceRev === undefined || (state.lastFullPassAt ?? 0) + fullIntervalMs <= now;

  // Walk the writer set. Entries must be strictly ascending by spaceRev and each
  // non-empty page's cursor must equal its last entry; a writer that reappears
  // keeps only its newest listing.
  const listed = new Map<string, ListedRepo>();
  let cursor = full ? undefined : state.spaceRev;
  while (true) {
    const page = yield* client.listRepos(space, cursor);
    if (page.repos.length === 0) break;
    for (const repo of page.repos) {
      if (cursor !== undefined && repo.spaceRev <= cursor) {
        return yield* new XrpcError({ method: LIST_REPOS, status: 200, error: "InvalidResponse", message: "listRepos entries are not in ascending spaceRev order" });
      }
      listed.delete(repo.did);
      listed.set(repo.did, { did: repo.did, repoRev: repo.repoRev, spaceRev: repo.spaceRev });
      cursor = repo.spaceRev;
    }
    if (page.cursor !== cursor) {
      return yield* new XrpcError({ method: LIST_REPOS, status: 200, error: "InvalidResponse", message: "listRepos cursor does not match its last entry" });
    }
  }

  const pending = [...listed.values()].sort((a, b) => (a.spaceRev < b.spaceRev ? -1 : 1));
  const results = yield* Effect.forEach(
    pending,
    (repo) =>
      Effect.gen(function* () {
        const local = yield* store.getRepo(space, repo.did);
        if (Option.isSome(local) && local.value.rev >= repo.repoRev) return { repo, ok: true as const, event: Option.none<SyncEvent>() };
        const event = yield* syncRepo(space, repo);
        return { repo, ok: true as const, event };
      }).pipe(
        // Per-repo failures don't fail the pass; the checkpoint below makes sure they're retried.
        Effect.catchTag(["RepoSyncError", "SinkError"], (error) =>
          Effect.logWarning("repo sync failed", error.message).pipe(
            Effect.annotateLogs({ space, did: repo.did }),
            Effect.as({ repo, ok: false as const, event: Option.none<SyncEvent>(), error: error.message }),
          ),
        ),
      ),
    { concurrency: config.repoConcurrency },
  );

  const events: SyncEvent[] = results.flatMap((r) => (Option.isSome(r.event) ? [r.event.value] : []));
  const firstFailure = results.findIndex((r) => !r.ok);

  if (full) {
    for (const did of yield* store.listRepoDids(space)) {
      if (listed.has(did)) continue;
      yield* Effect.uninterruptible(
        sink.apply({ _tag: "RepoRemoved", space, did }).pipe(Effect.andThen(store.removeRepo(space, did))),
      );
      events.push({ _tag: "RepoRemoved", space, did });
    }
  }

  // Never checkpoint past a failed repo: resume from the last entry before it.
  const checkpoint =
    firstFailure === -1 ? (cursor ?? state.spaceRev) : firstFailure === 0 ? state.spaceRev : pending[firstFailure - 1].spaceRev;
  const failed = firstFailure !== -1;
  const failures = failed ? state.failures + 1 : 0;
  const lastFullPassAt = full ? now : state.lastFullPassAt;
  const candidates = [(lastFullPassAt ?? now) + fullIntervalMs];
  if (state.registrationExpiresAt !== undefined) candidates.push(state.registrationExpiresAt - renewLeadMs);
  if (failed) candidates.push(now + backoffMillis(config, failures));
  const failedResult = failed ? results[firstFailure] : undefined;

  yield* store.putSpace({
    ...state,
    spaceRev: checkpoint,
    lastFullPassAt,
    failures,
    lastError: failedResult && "error" in failedResult ? `${failedResult.repo.did}: ${failedResult.error}` : undefined,
    nextDueAt: Math.min(...candidates),
  });
  return events as ReadonlyArray<SyncEvent>;
});

/** Record a failed pass on the space and schedule a backed-off retry. Never fails. */
export const recordPassFailure = (space: string, error: unknown) =>
  Effect.gen(function* () {
    const store = yield* SyncStore;
    const config = yield* SpaceSyncConfig;
    yield* Effect.logWarning("space pass failed", errorMessage(error)).pipe(Effect.annotateLogs({ space }));
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
  }).pipe(Effect.catch((e) => Effect.logError("could not record pass failure", errorMessage(e))));