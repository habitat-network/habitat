import { useQuery } from "@tanstack/react-query";
import { network } from "api";
import { xrpc } from "@atproto/lex";
import { AvatarGroup, AvatarGroupCount, Spinner } from "./ui";
import { UserAvatar } from "./UserAvatar";
import { AuthManager } from "../authManager";
import { useActors } from "../hooks/useActors";

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
  // This query only resolves the DID list (clique members expanded, direct
  // grantee DIDs kept); turning those DIDs into profiles is useActors's
  // job, which batches and caches per DID.
  const { data: dids = [], isLoading } = useQuery({
    queryKey: ["granteeDids", uri],
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
      return [
        ...new Set(
          cliqueMemberLists
            .flat()
            .concat(
              grantees?.filter((g) => "did" in g).map((g) => g.did) ?? [],
            ),
        ),
      ];
    },
  });
  const getActor = useActors(dids);

  if (isLoading) {
    return <Spinner />;
  }

  return (
    <AvatarGroup>
      {dids.slice(0, max).map((did) => (
        <UserAvatar size={size} actor={getActor(did)} key={did} />
      ))}
      {max && dids.length > max && (
        <AvatarGroupCount>+{dids.length - max}</AvatarGroupCount>
      )}
    </AvatarGroup>
  );
};

export default GranteeAvatars;