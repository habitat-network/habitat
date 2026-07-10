import { useQuery } from "@tanstack/react-query";
import { NetworkHabitatRepoGetRecord } from "api";
import { AvatarGroup, AvatarGroupCount, Spinner } from "./ui";
import { UserAvatar } from "./UserAvatar";
import { query, type Fetcher } from "../habitatClient";
import { getProfiles } from "../bskyPublicApi";
import { Actor } from "@/types/Actor";

interface GranteeAvatarProps {
  uri: string;
  grantees: NetworkHabitatRepoGetRecord.OutputSchema["permissions"];
  fetcher: Fetcher;
  max?: number;
  size?: "sm" | "lg" | "default";
}

const GranteeAvatars = ({
  uri,
  grantees,
  fetcher,
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
              { fetcher },
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
      return getProfiles(actors);
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
