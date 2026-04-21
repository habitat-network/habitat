import { QueryClient, queryOptions } from "@tanstack/react-query";
import { AuthManager } from "internal";

export const profileQueryOptions = (did: string, _authManager: AuthManager) =>
  queryOptions({
    queryKey: ["profile", did],
    queryFn: async () => {
      const params = new URLSearchParams({ actor: did });
      const res = await fetch(
        `https://public.api.bsky.app/xrpc/app.bsky.actor.getProfile?${params.toString()}`,
      );
      return res.json();
    },
  });

export const profilesQueryOptions = (
  dids: string[],
  _authManager: AuthManager,
  queryClient?: QueryClient,
) => {
  return queryOptions({
    queryKey: ["profiles", dids],
    queryFn: async () => {
      const params = new URLSearchParams();
      dids.forEach((d) => params.append("actors", d));
      const res = await fetch(
        `https://public.api.bsky.app/xrpc/app.bsky.actor.getProfiles?${params.toString()}`,
      );
      const { profiles } = await res.json();

      if (queryClient) {
        profiles.forEach((profile: { did: string }) => {
          queryClient?.setQueryData(["profile", profile.did], profile);
        });
      }
      return profiles;
    },
  });
};
