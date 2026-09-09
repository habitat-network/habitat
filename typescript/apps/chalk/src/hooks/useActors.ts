import { useCallback, useMemo } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
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
// The lookup reads straight from the query cache via getQueryData rather
// than materializing a Map: `useQuery` below already subscribes this
// component to the fetch's result (so it re-renders when profiles arrive),
// and getQueryData for that same key is guaranteed up to date on that
// render — no separate memoized structure needed just to index into it.
//
// dids is deduped and sorted before becoming the query key, so callers
// don't need to memoize it themselves to get a stable cache entry — the
// common case is deriving it fresh from other query data on every render
// (e.g. `comments.map(c => c.authorDid)`).
export function useActors(dids: string[]): (did: string) => Actor {
  const queryClient = useQueryClient();
  const key = useMemo(() => Array.from(new Set(dids)).sort(), [dids]);

  // The query's own `data` isn't read here — subscribing is enough to get
  // this component re-rendered when it resolves; getActor reads the same
  // cache entry directly.
  useQuery({
    queryKey: ["profiles", key],
    queryFn: () => getProfiles(key),
    enabled: key.length > 0,
  });

  return useCallback(
    (did: string): Actor =>
      queryClient
        .getQueryData<Actor[]>(["profiles", key])
        ?.find((actor) => actor.did === did) ?? { did },
    [queryClient, key],
  );
}
