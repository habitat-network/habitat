import { queryOptions, type QueryClient } from "@tanstack/react-query";
import { getProfile, getProfiles, type Actor } from "internal";

// Bluesky profiles change rarely and the same DID is hovered repeatedly across
// a spaces listing, so keep a fetched profile fresh for a while.
const PROFILE_STALE_TIME = 5 * 60 * 1000;

// profileQueryOptions fetches a DID's public Bluesky profile. The lookup goes
// to the public appview, so it needs no auth and works for any DID — including
// accounts that are not part of this org.
export function profileQueryOptions(did: string) {
  return queryOptions({
    queryKey: ["profile", did],
    queryFn: (): Promise<Actor> => getProfile(did),
    staleTime: PROFILE_STALE_TIME,
  });
}

// profilesQueryOptions batch-fetches public Bluesky profiles for a list of
// DIDs in a single request, seeding the per-DID profileQueryOptions cache so
// anything that later hovers one of these DIDs (e.g. DidHoverCard) reuses the
// result instead of re-fetching.
export function profilesQueryOptions(dids: string[], queryClient: QueryClient) {
  return queryOptions({
    queryKey: ["profiles", dids],
    queryFn: async (): Promise<Actor[]> => {
      const profiles = await getProfiles(dids);
      profiles.forEach((profile) => {
        queryClient.setQueryData(["profile", profile.did], profile);
      });
      return profiles;
    },
    staleTime: PROFILE_STALE_TIME,
  });
}
