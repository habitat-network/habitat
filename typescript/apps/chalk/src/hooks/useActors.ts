import { useCallback, useMemo } from "react";
import { useQueries } from "@tanstack/react-query";
import { AsyncBatcher } from "@tanstack/pacer/async-batcher";
import { getProfiles, type Actor } from "internal";

// useActors resolves a list of DIDs to their public bsky profiles and
// returns a lookup function — the "who is this" display need shared by
// every place chalk shows an avatar/name for a DID it only has as a bare
// string (comment/reply authors, doc owners, doc-access grantees). A did
// whose profile lookup didn't resolve (or hasn't finished yet) gets back a
// bare `{ did }` Actor rather than undefined, so callers can pass the
// result straight to UserAvatar/UserDisplayName without their own
// fallback.
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
type Deferred<T> = {
  promise: Promise<T>;
  resolve: (value: T) => void;
  reject: (reason?: unknown) => void;
};

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

type ActorRequest = {
  deferred: Deferred<Actor>;
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
  const d = deferred<Actor>();
  actorBatcher.addItem({ deferred: d, did });
  return d.promise;
}

export function useActors(dids: string[]): (did: string) => Actor {
  // Deduped before becoming query keys so callers don't need to memoize
  // the list themselves — the common case is deriving it fresh from other
  // query data on every render (e.g. `comments.map(c => c.authorDid)`).
  const key = useMemo(() => Array.from(new Set(dids)), [dids]);

  // One query per did. A profile is read-mostly data, so keep entries warm
  // across mounts rather than refetching them on every visit.
  const results = useQueries({
    queries: key.map((did) => ({
      queryKey: ["actor", did],
      queryFn: () => loadActor(did),
      staleTime: 5 * 60 * 1000,
    })),
  });

  const actorsByDid = useMemo(() => {
    const map = new Map<string, Actor>();
    key.forEach((did, i) => {
      map.set(did, results[i]?.data ?? { did });
    });
    return map;
  }, [key, results]);

  return useCallback(
    (did: string): Actor => actorsByDid.get(did) ?? { did },
    [actorsByDid],
  );
}
