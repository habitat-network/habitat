import { useCallback, useMemo } from "react";
import { useQueries, useQueryClient } from "@tanstack/react-query";
import { AsyncBatcher } from "@tanstack/pacer/async-batcher";
import { getProfiles as getBskyProfiles } from "../bskyPublicApi";
import type { Actor } from "../types/Actor";

// useActors resolves a list of DIDs to their public bsky profiles and
// returns a lookup function — the "who is this" display need shared by
// every place an app shows an avatar/name for a DID it only has as a bare
// string (doc owners, comment authors, permission grantees). A did whose
// profile lookup didn't resolve (or hasn't finished yet) gets back a bare
// `{ did }` Actor rather than undefined, so callers can pass the result
// straight to UserAvatar/UserDisplayName without their own fallback.
//
// Unlike a single ["profiles", dids] query keyed on the whole list, each
// did gets its own ["actor", did] cache entry. That way the cache is
// shared across every component that shows profiles (the same did
// rendered in two places is one query, not one per list it appears in),
// and a component re-listing a subset of dids doesn't refetch the others.
//
// The dids themselves are still fetched in batches rather than one
// getProfiles call per did: loadActor below hands the did to its source's
// module-wide AsyncBatcher, which coalesces every query that fires in the same tick
// into a single getProfiles request (this is the pattern from TanStack's
// react/batching example).
type ActorRequest = {
  deferred: PromiseWithResolvers<Actor>;
  did: string;
};

// ActorSource is where useActors gets profiles from. The default is the
// public bsky API; an app whose members live in a Habitat org passes a source
// backed by network.habitat.opensocial.getProfiles instead, so users are shown
// the way the org sees them.
//
// `key` names the source in the query cache and picks its batcher, so
// sources that can answer differently for the same did (say, two orgs) don't
// share cache entries. Use a stable `fetchProfiles` per key; it gets every
// did requested in the same tick and may omit the ones it can't resolve.
export interface ActorSource {
  key: string;
  fetchProfiles: (dids: string[]) => Promise<Actor[]>;
}

const bskySource: ActorSource = {
  key: "bsky",
  fetchProfiles: getBskyProfiles,
};

const batchers = new Map<string, AsyncBatcher<ActorRequest>>();

function batcherFor(source: ActorSource): AsyncBatcher<ActorRequest> {
  let batcher = batchers.get(source.key);
  if (!batcher) {
    batcher = new AsyncBatcher<ActorRequest>(
      // Fetch every did in a batch once (the batcher may see duplicates
      // across callers), indexed by did so onSuccess can hand each requester
      // its own profile.
      async (requests) => {
        const profiles = await source.fetchProfiles([
          ...new Set(requests.map(({ did }) => did)),
        ]);
        return new Map(profiles.map((actor) => [actor.did, actor]));
      },
      {
        wait: 0,
        onSuccess: (actorsById, requests) => {
          for (const { deferred, did } of requests) {
            deferred.resolve(actorsById.get(did) ?? { did });
          }
        },
        onError: (error, requests) => {
          for (const { deferred } of requests) {
            deferred.reject(error);
          }
        },
      },
    );
    batchers.set(source.key, batcher);
  }
  return batcher;
}

// loadActor is what each per-did query's queryFn calls; it enqueues the did
// on the source's shared batcher and resolves with that did's profile (or a
// bare `{ did }` when the source returned no profile for it).
function loadActor(source: ActorSource, did: string): Promise<Actor> {
  const deferred = Promise.withResolvers<Actor>();
  batcherFor(source).addItem({ deferred, did });
  return deferred.promise;
}

export function useActors(
  dids: string[],
  source: ActorSource = bskySource,
): (did: string) => Actor {
  const queryClient = useQueryClient();
  // Deduped before becoming query keys so callers don't need to memoize
  // the list themselves — the common case is deriving it fresh from other
  // query data on every render (e.g. `comments.map(c => c.authorDid)`).
  const key = useMemo(() => Array.from(new Set(dids)), [dids]);

  // The queries' own `data` isn't read here — subscribing is enough to get
  // this component re-rendered when a profile resolves, and getActor reads
  // the same ["actor", did] cache entry directly. That also means the
  // lookup resolves any did's profile, whether it was in this component's
  // list or is cached here by another useActors caller.
  //
  // One query per did, kept warm across mounts: a profile is read-mostly
  // data, and the per-did keys let the cache be shared by every component
  // that shows profiles.
  useQueries({
    queries: key.map((did) => ({
      queryKey: ["actor", source.key, did],
      queryFn: () => loadActor(source, did),
      staleTime: 5 * 60 * 1000,
    })),
  });

  return useCallback(
    (did: string): Actor =>
      queryClient.getQueryData<Actor>(["actor", source.key, did]) ?? { did },
    [queryClient, source.key],
  );
}
