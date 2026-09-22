import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { orgMembersQueryOptions } from "@/queries/opensocial";
import { orgMcpServersQueryOptions } from "@/queries/mcp";
import { McpServersEditor } from "@/components/McpServersEditor";

export const Route = createFileRoute("/_requireAuth/opensocial/$org/mcp")({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      orgMcpServersQueryOptions(params.org, context.authManager),
    ),
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
  const isAdmin = members.some(
    (m) =>
      m.did === authManager.getAuthInfo()?.did && m.roles.includes("admin"),
  );

  return (
    <McpServersEditor
      org={org}
      servers={servers}
      isAdmin={isAdmin}
      authManager={authManager}
    />
  );
}
