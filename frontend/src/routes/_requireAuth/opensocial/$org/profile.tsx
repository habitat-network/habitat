import { createFileRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { getProfileQueryOptions } from "@/queries/org";
import { MemberProfileForm } from "@/components/MemberProfileForm";

export const Route = createFileRoute("/_requireAuth/opensocial/$org/profile")(
  {
    loader: ({ context }) => {
      const did = context.authManager.getAuthInfo()!.did;
      return context.queryClient.ensureQueryData(
        getProfileQueryOptions(context.authManager, did),
      );
    },
    component: MyProfile,
  },
);

function MyProfile() {
  const { org } = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const did = authManager.getAuthInfo()!.did;
  const { data: profile } = useQuery(
    getProfileQueryOptions(authManager, did),
  );

  return (
    <div className="flex flex-col gap-4">
      <h2 className="text-base font-semibold">My profile</h2>
      <MemberProfileForm
        org={org}
        did={did}
        initialDisplayName={profile?.displayName ?? ""}
        initialBio={profile?.bio ?? ""}
        initialAvatarUrl={profile?.avatarUrl ?? ""}
        authManager={authManager}
      />
    </div>
  );
}
