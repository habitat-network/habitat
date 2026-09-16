import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { orgProfileQueryOptions } from "@/queries/opensocial";
import { OrgProfileForm } from "@/components/OrgProfileForm";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "internal/components/ui";

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
    <Card size="sm">
      <CardHeader>
        <CardTitle className="text-base">Community profile</CardTitle>
      </CardHeader>
      <CardContent>
        <OrgProfileForm
          org={org}
          initialName={profile?.name ?? ""}
          initialDescription={profile?.description ?? ""}
          avatarUrl={profile?.avatarUrl}
          authManager={authManager}
        />
      </CardContent>
    </Card>
  );
}
