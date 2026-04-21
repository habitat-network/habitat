import { useQuery } from "@tanstack/react-query";
import { NetworkHabitatRepoGetRecord } from "api";
import { AvatarGroup, AvatarGroupCount, Spinner } from "./ui";
import { UserAvatar } from "./UserAvatar";
import { query } from "../habitatClient";
import { AuthManager } from "../authManager";
import { Actor } from "@/types/Actor";

interface GranteeAvatarProps {
  uri: string;
  grantees: NetworkHabitatRepoGetRecord.OutputSchema["permissions"];
  authManager: AuthManager;
  max?: number;
  size?: "sm" | "lg" | "default";
}

const GranteeAvatars = ({
  uri,
  grantees,
  authManager,
  max,
  size = "default",
}: GranteeAvatarProps) => {
  const { data: profiles, isLoading } = useQuery({
    queryKey: ["granteeProfiles", uri],
    queryFn: async () => {
      const cliqueMemberLists = await Promise.all(
        grantees
          ?.filter((g) => "clique" in g)
          .map(async (g) => {
            const { members } = await query(
              "network.habitat.clique.getMembers",
              {
                clique: g.clique,
              },
              { authManager },
            );
            return members;
          }) ?? [],
      );
      const actors = [
        ...new Set(
          cliqueMemberLists
            .flat()
            .concat(
              grantees?.filter((g) => "did" in g).map((g) => g.did) ?? [],
            ),
        ),
      ];
      const params = new URLSearchParams();
      actors.forEach((a) => params.append("actors", a));
      const profilesRes = await fetch(
        `https://public.api.bsky.app/xrpc/app.bsky.actor.getProfiles?${params.toString()}`,
      );
      const { profiles } = await profilesRes.json();
      return profiles;
    },
  });

  if (isLoading) {
    return <Spinner />;
  }

  return (
    <AvatarGroup>
      {profiles?.slice(0, max).map((p: Actor) => (
        <UserAvatar size={size} actor={p} key={p.did} />
      ))}
      {profiles && max && profiles.length > max && (
        <AvatarGroupCount>+{profiles.length - max}</AvatarGroupCount>
      )}
    </AvatarGroup>
  );
};

export default GranteeAvatars;
