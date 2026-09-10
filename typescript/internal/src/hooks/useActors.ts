import { useCallback, useMemo } from "react";
import { useQueries, useQueryClient } from "@tanstack/react-query";
import { AsyncBatcher } from "@tanstack/pacer/async-batcher";
import { getProfiles } from "../bskyPublicApi";
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
// getProfiles call per did: loadActor below hands the did to a module-wide
// AsyncBatcher, which coalesces every query that fires in the same tick
// into a single getProfiles request (this is the pattern from TanStack's
// react/batching example).
type ActorRequest = {
  deferred: PromiseWithResolvers<Actor>;
  did: string;
};

// fetchBatchProfiles fetches every did in a batch once (the batcher may
// see duplicates across callers) and returns them indexed by did so
// onSuccess can hand each requester its own profile.
const fetchBatchProfiles = async (
  dids: ReadonlyArray<string>,
): Promise<Map<string, Actor>> => {
  const profiles = await getProfiles([...new Set(dids)]);
  return new Map(profiles.map((actor) => [actor.did, actor]));
};

const actorBatcher = new AsyncBatcher<ActorRequest>(
  (requests) => fetchBatchProfiles(requests.map(({ did }) => did)),
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

// loadActor is what each per-did query's queryFn calls; it enqueues the did
// on the shared batcher and resolves with that did's profile (or a bare
// `{ did }` when bsky returned no profile for it).
function loadActor(did: string): Promise<Actor> {
  const deferred = Promise.withResolvers<Actor>();
  actorBatcher.addItem({ deferred, did });
  return deferred.promise;
}

export function useActors(dids: string[]): (did: string) => Actor {
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
      queryKey: ["actor", did],
      queryFn: () => loadActor(did),
      staleTime: 5 * 60 * 1000,
    })),
  });

  return useCallback(
    (did: string): Actor =>
      queryClient.getQueryData<Actor>(["actor", did]) ?? { did },
    [queryClient],
  );
}
