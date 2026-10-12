import { createFileRoute } from "@tanstack/react-router";
import { useSuspenseQuery } from "@tanstack/react-query";
import { MemberProfileForm } from "@/components/MemberProfileForm";
import { myMemberProfileQueryOptions } from "@/queries/opensocial";

export const Route = createFileRoute("/_requireAuth/orgs/$org/profile")({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      myMemberProfileQueryOptions(params.org, context.authManager),
    ),
  component: MyProfile,
});

function MyProfile() {
  const { org } = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const { data: profile } = useSuspenseQuery(
    myMemberProfileQueryOptions(org, authManager),
  );

  return (
    <div className="mx-auto flex max-w-xl flex-col gap-4 py-6 px-4">
      <h2 className="text-base font-semibold">Your profile</h2>
      <p className="text-sm text-muted-foreground">
        This is how other members of this community see you.
      </p>
      <MemberProfileForm
        org={org}
        did={authManager.getAuthInfo()!.did}
        initial={profile}
        authManager={authManager}
      />
    </div>
  );
}
