import { useQuery } from "@tanstack/react-query";
import { network } from "api";
import { xrpc, type DidString } from "@atproto/lex";
import { AvatarGroup, AvatarGroupCount, Spinner } from "./ui";
import { UserAvatar } from "./UserAvatar";
import { getProfiles } from "../bskyPublicApi";
import { AuthManager } from "../authManager";
import { Actor } from "@/types/Actor";

type Grantee = Exclude<
  network.habitat.repo.getRecord.$OutputBody["permissions"],
  undefined
>[number];

interface GranteeAvatarProps {
  uri: string;
  grantees: Grantee[] | undefined;
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
            const rsp = await xrpc(
              authManager,
              network.habitat.clique.getMembers.main,
              { params: { clique: g.clique } },
            );
            return rsp.body.members;
          }) ?? [],
      );
      const actors: DidString[] = [
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
