import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { getProfiles, type Actor } from "internal";

// useActors resolves a list of DIDs to their public bsky profiles, keyed by
// did for O(1) lookup — the "who is this" display need shared by every
// place chalk shows an avatar/name for a DID it only has as a bare string
// (comment/reply authors, doc owners, doc-access grantees). A did missing
// from the returned map means its profile lookup didn't resolve (or hasn't
// finished yet); callers fall back to a bare `{ did }` Actor themselves
// rather than this hook synthesizing placeholder profiles.
//
// dids is deduped and sorted before becoming the query key, so callers
// don't need to memoize it themselves to get a stable cache entry — the
// common case is deriving it fresh from other query data on every render
// (e.g. `comments.map(c => c.authorDid)`).
export function useActors(dids: string[]): Map<string, Actor> {
  const key = useMemo(() => Array.from(new Set(dids)).sort(), [dids]);
  const { data: actors = [] } = useQuery({
    queryKey: ["profiles", key],
    queryFn: () => getProfiles(key),
    enabled: key.length > 0,
  });
  return useMemo(
    () => new Map(actors.map((actor) => [actor.did, actor] as const)),
    [actors],
  );
}
