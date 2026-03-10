import { QueryClient, queryOptions } from "@tanstack/react-query";
import { AuthManager, query } from "internal";

export const profileQueryOptions = (did: string, authManager: AuthManager) =>
  queryOptions({
    queryKey: ["profile", did],
    queryFn: () =>
      query("app.bsky.actor.getProfile", { actor: did }, { authManager }),
  });

export const profilesQueryOptions = (
  dids: string[],
  authManager: AuthManager,
  queryClient?: QueryClient,
) => {
  return queryOptions({
    queryKey: ["profiles", dids],
    queryFn: async () => {
      const { profiles } = await query(
        "app.bsky.actor.getProfiles",
        { actors: dids },
        { authManager },
      );

      if (queryClient) {
        profiles.forEach((profile) => {
          queryClient?.setQueryData(["profile", profile.did], profile);
        });
      }
      return profiles;
    },
  });
};
