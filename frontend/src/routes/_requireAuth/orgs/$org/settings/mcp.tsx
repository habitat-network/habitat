import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  orgMembersQueryOptions,
  orgPermissionsQueryOptions,
} from "@/queries/opensocial";
import { orgMcpServersQueryOptions } from "@/queries/mcp";
import { McpServersEditor } from "@/components/McpServersEditor";
import {
  ACTION_MCP_CONFIGURE,
  hasOpensocialAction,
} from "@/lib/opensocialActions";

export const Route = createFileRoute("/_requireAuth/orgs/$org/settings/mcp")({
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(
        orgMcpServersQueryOptions(params.org, context.authManager),
      ),
      context.queryClient.ensureQueryData(
        orgPermissionsQueryOptions(
          params.org,
          context.authManager,
          context.queryClient,
        ),
      ),
    ]),
  component: OrgMcpServers,
});

function OrgMcpServers() {
  const { org } = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const { data: servers = [] } = useQuery(
    orgMcpServersQueryOptions(org, authManager),
  );
  const { data: members = [] } = useQuery(
    orgMembersQueryOptions(org, authManager, queryClient),
  );
  const { data: permissions } = useQuery(
    orgPermissionsQueryOptions(org, authManager, queryClient),
  );
  const userRoles =
    members.find((m) => m.did === authManager.getAuthInfo()?.did)?.roles ?? [];
  const canConfigureMcp = hasOpensocialAction(
    permissions?.bindings ?? [],
    userRoles,
    ACTION_MCP_CONFIGURE,
  );

  return (
    <McpServersEditor
      org={org}
      servers={servers}
      canConfigureMcp={canConfigureMcp}
      authManager={authManager}
    />
  );
}
