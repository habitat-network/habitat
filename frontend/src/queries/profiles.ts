import { queryOptions, type QueryClient } from "@tanstack/react-query";
import { type AuthManager, type Actor } from "internal";
import { xrpc, type DidString } from "@atproto/lex";
import { network } from "api";
import { ensureValidDid } from "@atproto/syntax";
import { pearAgent } from "./pearAgent";

// Profiles change rarely and the same DID is hovered repeatedly across a
// listing, so keep a fetched profile fresh for a while.
const PROFILE_STALE_TIME = 5 * 60 * 1000;

// fetchOrgProfiles asks Habitat's network.habitat.opensocial.getProfiles for
// the profiles of dids as members of org. DIDs that aren't members of org are
// omitted by the server, so callers must tolerate missing entries.
async function fetchOrgProfiles(
  org: string,
  dids: string[],
  authManager: AuthManager,
): Promise<Actor[]> {
  if (dids.length === 0) return [];
  ensureValidDid(org);
  const response = await xrpc(
    pearAgent(authManager),
    network.habitat.opensocial.getProfiles.main,
    { params: { org: org as DidString, dids: dids as DidString[] } },
  );
  return response.body.profiles.map((p) => ({
    did: p.did,
    handle: p.handle,
    displayName: p.displayName,
    avatar: p.avatarUrl,
  }));
}

// profileQueryOptions fetches a DID's profile as a member of org, the way the
// org's other members see it.
export function profileQueryOptions(
  org: string,
  did: string,
  authManager: AuthManager,
) {
  return queryOptions({
    queryKey: ["profile", org, did],
    queryFn: async (): Promise<Actor> => {
      const [profile] = await fetchOrgProfiles(org, [did], authManager);
      return profile ?? { did };
    },
    staleTime: PROFILE_STALE_TIME,
  });
}

// profilesQueryOptions batch-fetches org members' profiles for a list of DIDs
// in a single request, seeding the per-DID profileQueryOptions cache so
// anything that later hovers one of these DIDs (e.g. DidHoverCard) reuses the
// result instead of re-fetching.
export function profilesQueryOptions(
  org: string,
  dids: string[],
  authManager: AuthManager,
  queryClient: QueryClient,
) {
  return queryOptions({
    queryKey: ["profiles", org, dids],
    queryFn: async (): Promise<Actor[]> => {
      const profiles = await fetchOrgProfiles(org, dids, authManager);
      profiles.forEach((profile) => {
        queryClient.setQueryData(["profile", org, profile.did], profile);
      });
      return profiles;
    },
    staleTime: PROFILE_STALE_TIME,
  });
}
