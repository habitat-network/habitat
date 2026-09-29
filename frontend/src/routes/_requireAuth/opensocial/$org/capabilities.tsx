import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  orgPermissionsQueryOptions,
  orgRolesQueryOptions,
} from "@/queries/opensocial";
import { CapabilitiesEditor } from "@/components/CapabilitiesEditor";

export const Route = createFileRoute(
  "/_requireAuth/opensocial/$org/capabilities",
)({
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(
        orgRolesQueryOptions(
          params.org,
          context.authManager,
          context.queryClient,
        ),
      ),
      context.queryClient.ensureQueryData(
        orgPermissionsQueryOptions(
          params.org,
          context.authManager,
          context.queryClient,
        ),
      ),
    ]),
  component: OrgCapabilities,
});

function OrgCapabilities() {
  const { org } = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const { data: roles = [] } = useQuery(
    orgRolesQueryOptions(org, authManager, queryClient),
  );
  const { data: permissions } = useQuery(
    orgPermissionsQueryOptions(org, authManager, queryClient),
  );

  return (
    <CapabilitiesEditor
      org={org}
      roles={roles}
      bindings={permissions?.bindings ?? []}
      assignable={permissions?.assignable ?? []}
      authManager={authManager}
    />
  );
}
