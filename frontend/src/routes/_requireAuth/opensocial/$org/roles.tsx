import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { orgRolesQueryOptions } from "@/queries/opensocial";
import { RolesEditor } from "@/components/RolesEditor";

export const Route = createFileRoute("/_requireAuth/opensocial/$org/roles")({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      orgRolesQueryOptions(
        params.org,
        context.authManager,
        context.queryClient,
      ),
    ),
  component: OrgRoles,
});

function OrgRoles() {
  const { org } = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const { data: roles = [] } = useQuery(
    orgRolesQueryOptions(org, authManager, queryClient),
  );

  return <RolesEditor org={org} roles={roles} authManager={authManager} />;
}
