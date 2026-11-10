import { createFileRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { orgMcpServersQueryOptions } from "@/queries/mcp";
import { McpDebugger } from "@/components/McpDebugger";

export const Route = createFileRoute(
  "/_requireAuth/orgs/$org/settings/mcp-debug",
)({
  validateSearch: z.object({ server: z.string().optional() }),
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      orgMcpServersQueryOptions(params.org, context.authManager),
    ),
  component: OrgMcpDebug,
});

function OrgMcpDebug() {
  const { org } = Route.useParams();
  const { server } = Route.useSearch();
  const { authManager } = Route.useRouteContext();
  const { data: servers = [] } = useQuery(
    orgMcpServersQueryOptions(org, authManager),
  );
  return (
    <McpDebugger
      org={org}
      servers={servers}
      authManager={authManager}
      initialServer={server}
    />
  );
}
