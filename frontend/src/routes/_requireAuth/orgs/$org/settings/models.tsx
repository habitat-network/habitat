import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  orgMembersQueryOptions,
  orgPermissionsQueryOptions,
} from "@/queries/opensocial";
import { orgLlmConnectionsQueryOptions } from "@/queries/llm";
import { LlmConnectionsEditor } from "@/components/LlmConnectionsEditor";
import {
  ACTION_COMMUNITY_CONFIGURE,
  hasOpensocialAction,
} from "@/lib/opensocialActions";

export const Route = createFileRoute("/_requireAuth/orgs/$org/settings/models")(
  {
    loader: ({ context, params }) =>
      Promise.all([
        context.queryClient.ensureQueryData(
          orgLlmConnectionsQueryOptions(params.org, context.authManager),
        ),
        context.queryClient.ensureQueryData(
          orgPermissionsQueryOptions(
            params.org,
            context.authManager,
            context.queryClient,
          ),
        ),
      ]),
    component: OrgModels,
  },
);

function OrgModels() {
  const { org } = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const { data: connections = [] } = useQuery(
    orgLlmConnectionsQueryOptions(org, authManager),
  );
  const { data: members = [] } = useQuery(
    orgMembersQueryOptions(org, authManager, queryClient),
  );
  const { data: permissions } = useQuery(
    orgPermissionsQueryOptions(org, authManager, queryClient),
  );
  const userRoles =
    members.find((m) => m.did === authManager.getAuthInfo()?.did)?.roles ?? [];
  // Model connections have no action of their own; they're part of
  // configuring the community, as search is.
  const canConfigure = hasOpensocialAction(
    permissions?.bindings ?? [],
    userRoles,
    ACTION_COMMUNITY_CONFIGURE,
  );

  return (
    <LlmConnectionsEditor
      org={org}
      connections={connections}
      canConfigure={canConfigure}
      authManager={authManager}
    />
  );
}
