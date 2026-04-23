import { QueryClient, queryOptions } from "@tanstack/react-query";
import { AuthManager, getProfile, getProfiles } from "internal";

export const profileQueryOptions = (did: string, _authManager: AuthManager) =>
  queryOptions({
    queryKey: ["profile", did],
    queryFn: () => getProfile(did),
  });

export const profilesQueryOptions = (
  dids: string[],
  _authManager: AuthManager,
  queryClient?: QueryClient,
) => {
  return queryOptions({
    queryKey: ["profiles", dids],
    queryFn: async () => {
      const profiles = await getProfiles(dids);

      if (queryClient) {
        profiles.forEach((profile) => {
          queryClient.setQueryData(["profile", profile.did], profile);
        });
      }
      return profiles;
    },
  });
};
