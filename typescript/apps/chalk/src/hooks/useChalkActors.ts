import { useMemo } from "react";
import { getRouteApi } from "@tanstack/react-router";
import type { ActorSource } from "internal/hooks";
import { useActors } from "internal/hooks";
import { getProfiles } from "@/server/functions";

const requireAuthRoute = getRouteApi("/_requireAuth");

// useChalkActors is useActors backed by chalk's getProfiles server function,
// so every user chalk shows is resolved through Habitat's getProfiles (as a
// member of the current org, when there is one). The org is part of the
// cache key: the same did can look different in another org.
export function useChalkActors(dids: string[]) {
  const { currentOrg } = requireAuthRoute.useLoaderData();
  const source: ActorSource = useMemo(
    () => ({
      key: `chalk:${currentOrg?.did ?? "personal"}`,
      fetchProfiles: (batch) => getProfiles({ data: { dids: batch } }),
    }),
    [currentOrg?.did],
  );
  return useActors(dids, source);
}
