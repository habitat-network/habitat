import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { orgProfileQueryOptions } from "@/queries/opensocial";
import { OrgProfileForm } from "@/components/OrgProfileForm";

export const Route = createFileRoute("/_requireAuth/opensocial/$org/settings")({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      orgProfileQueryOptions(
        params.org,
        context.authManager,
        context.queryClient,
      ),
    ),
  component: OrgSettings,
});

function OrgSettings() {
  const { org } = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const { data: profile } = useQuery(
    orgProfileQueryOptions(org, authManager, queryClient),
  );

  return (
    <div className="flex flex-col gap-4">
      <h2 className="text-base font-semibold">Community profile</h2>
      <OrgProfileForm
        org={org}
        initialName={profile?.name ?? ""}
        initialDescription={profile?.description ?? ""}
        avatarUrl={profile?.avatarUrl}
        authManager={authManager}
      />
    </div>
  );
}
