import { useCallback } from "react";
import { useQueries } from "@tanstack/react-query";
import type { Actor } from "internal";
import { useActors } from "internal/hooks";

// Habitat server that resolves identities bsky's public API doesn't know.
// VITE_HABITAT_DOMAIN is only set by moon's dev task (moon.yml), pointing at
// local pear — the only server that knows local handles. Production builds
// leave it unset, so callers fall back to their own default.
export const identityResolverUrl = import.meta.env.VITE_HABITAT_DOMAIN
  ? `https://${import.meta.env.VITE_HABITAT_DOMAIN}`
  : undefined;

async function resolveHandle(did: string): Promise<Actor> {
  const res = await fetch(
    `${identityResolverUrl ?? "https://pear.habitat.network"}/xrpc/com.atproto.identity.resolveIdentity?identifier=${encodeURIComponent(did)}`,
  );
  if (!res.ok) return { did };
  const { handle } = (await res.json()) as { handle?: string };
  return { did, handle };
}

// useHabitatActors is useActors plus a fallback for DIDs bsky has no profile
// for (did:web accounts hosted on a Habitat instance): their handle is
// resolved through Habitat, so they show as a handle rather than a raw DID.
export function useHabitatActors(dids: string[]): (did: string) => Actor {
  const getActor = useActors(dids);
  const unresolved = dids.filter((did) => !getActor(did).handle);
  const fallbacks = useQueries({
    queries: Array.from(new Set(unresolved)).map((did) => ({
      queryKey: ["habitat-actor", did],
      queryFn: () => resolveHandle(did),
      staleTime: 5 * 60 * 1000,
    })),
  });
  const byDid = new Map(
    fallbacks.flatMap((q) => (q.data ? [[q.data.did, q.data]] : [])),
  );
  return useCallback(
    (did: string) =>
      getActor(did).handle ? getActor(did) : (byDid.get(did) ?? getActor(did)),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [getActor, fallbacks.map((q) => q.data?.handle).join()],
  );
}
