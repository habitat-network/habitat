import { Context, Effect, Layer, Option } from "effect";
import type { StoreError } from "./errors";
import type { RepoState, SpaceState } from "./types";

/** Host-provided durable sync state. Implementations should make each call atomic. */
export class SyncStore extends Context.Service<
  SyncStore,
  {
    readonly getSpace: (space: string) => Effect.Effect<Option.Option<SpaceState>, StoreError>;
    readonly putSpace: (state: SpaceState) => Effect.Effect<void, StoreError>;
    /** Also drops every RepoState of the space. */
    readonly removeSpace: (space: string) => Effect.Effect<void, StoreError>;
    /** Spaces with `nextDueAt <= now`, oldest first. */
    readonly dueSpaces: (now: number, limit: number) => Effect.Effect<ReadonlyArray<SpaceState>, StoreError>;
    readonly getRepo: (space: string, did: string) => Effect.Effect<Option.Option<RepoState>, StoreError>;
    readonly listRepoDids: (space: string) => Effect.Effect<ReadonlyArray<string>, StoreError>;
    readonly putRepo: (state: RepoState) => Effect.Effect<void, StoreError>;
    readonly removeRepo: (space: string, did: string) => Effect.Effect<void, StoreError>;
  }
>()("internal/spaceSync/SyncStore") {
  /** Process-local store for tests and ephemeral syncers. */
  static readonly memory = Layer.sync(SyncStore, () => {
    const spaces = new Map<string, SpaceState>();
    const repos = new Map<string, Map<string, RepoState>>();
    return SyncStore.of({
      getSpace: (space) => Effect.sync(() => Option.fromNullishOr(spaces.get(space))),
      putSpace: (state) => Effect.sync(() => void spaces.set(state.space, state)),
      removeSpace: (space) =>
        Effect.sync(() => {
          spaces.delete(space);
          repos.delete(space);
        }),
      dueSpaces: (now, limit) =>
        Effect.sync(() =>
          [...spaces.values()]
            .filter((s) => s.nextDueAt <= now)
            .sort((a, b) => a.nextDueAt - b.nextDueAt)
            .slice(0, limit),
        ),
      getRepo: (space, did) => Effect.sync(() => Option.fromNullishOr(repos.get(space)?.get(did))),
      listRepoDids: (space) => Effect.sync(() => [...(repos.get(space)?.keys() ?? [])]),
      putRepo: (state) =>
        Effect.sync(() => {
          let bySpace = repos.get(state.space);
          if (!bySpace) {
            bySpace = new Map();
            repos.set(state.space, bySpace);
          }
          bySpace.set(state.did, { ...state, ltHash: state.ltHash.slice() });
        }),
      removeRepo: (space, did) => Effect.sync(() => void repos.get(space)?.delete(did)),
    });
  });
}