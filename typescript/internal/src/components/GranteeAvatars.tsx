import { useQuery } from "@tanstack/react-query";
import { NetworkHabitatRepoGetRecord } from "api";
import { AvatarGroup, AvatarGroupCount, Spinner } from "./ui";
import { UserAvatar } from "./UserAvatar";
import { query } from "../habitatClient";
import { AuthManager } from "../authManager";

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
      const { profiles } = await query(
        "app.bsky.actor.getProfiles",
        {
          actors: [
            ...new Set(
              cliqueMemberLists
                .flat()
                .concat(
                  grantees?.filter((g) => "did" in g).map((g) => g.did) ?? [],
                ),
            ),
          ],
        },
        { authManager },
      );
      return profiles;
    },
  });

  if (isLoading) {
    return <Spinner />;
  }

  return (
    <AvatarGroup>
      {profiles?.slice(0, max).map((p) => (
        <UserAvatar size={size} actor={p} key={p.did} />
      ))}
      {profiles && max && profiles.length > max && (
        <AvatarGroupCount>+{profiles.length - max}</AvatarGroupCount>
      )}
    </AvatarGroup>
  );
};

export default GranteeAvatars;
