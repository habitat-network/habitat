import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import {
  Badge,
  Button,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "internal/components/ui";
import { Plug, Plus } from "lucide-react";
import {
  EmptyState,
  PageBody,
  PageHeader,
} from "@/components/shell/PageHeader";
import { McpServerIcon } from "@/components/mcp/McpServerIcon";
import {
  AUTH_LABELS,
  mcpServerDisplay,
  sortMcpServers,
} from "@/lib/mcpConnectors";
import { groupQuery, mcpServersQuery } from "@/lib/queries";

// MCP servers: the tools the group's members reach through pear's MCP
// gateway. Admins (mcp.configure) add and remove servers; every member
// connects their own account to each one. Click a row for its details.

export const Route = createFileRoute("/_member/g/$org/mcp/")({
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(groupQuery(params.org)),
      context.queryClient.ensureQueryData(mcpServersQuery(params.org)),
    ]),
  component: McpPage,
});

function McpPage() {
  const { org } = Route.useParams();
  const navigate = useNavigate();
  const { data: group } = useSuspenseQuery(groupQuery(org));
  const { data } = useSuspenseQuery(mcpServersQuery(org));
  const servers = sortMcpServers(data);
  const canConfigure = group.myActions.includes("mcp.configure");

  return (
    <>
      <PageHeader
        title="MCP servers"
        description="Tools your group's AI clients can use through Habitat"
      >
        {canConfigure && (
          <Button render={<Link to="/g/$org/mcp/new" params={{ org }} />}>
            <Plus data-icon="inline-start" />
            Add server
          </Button>
        )}
      </PageHeader>
      <PageBody>
        {servers.length === 0 ? (
          <EmptyState icon={<Plug />} title="No MCP servers yet">
            {canConfigure
              ? "Add one to give members its tools."
              : "An admin can add servers here."}
          </EmptyState>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Server</TableHead>
                <TableHead>Sign-in</TableHead>
                <TableHead>Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {servers.map(({ server, connected }) => {
                const display = mcpServerDisplay(server);
                return (
                  <TableRow
                    key={server.id}
                    className="cursor-pointer"
                    onClick={() =>
                      navigate({
                        to: "/g/$org/mcp/$serverId",
                        params: { org, serverId: server.id },
                      })
                    }
                  >
                    <TableCell>
                      <div className="flex items-center gap-3">
                        <McpServerIcon icon={display.icon} />
                        <div className="min-w-0">
                          <Link
                            to="/g/$org/mcp/$serverId"
                            params={{ org, serverId: server.id }}
                            className="font-medium hover:underline"
                            onClick={(e) => e.stopPropagation()}
                          >
                            {display.name}
                          </Link>
                          {server.description && (
                            <p className="max-w-md truncate text-xs text-muted-foreground">
                              {server.description}
                            </p>
                          )}
                        </div>
                      </div>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {AUTH_LABELS[server.authType] ?? server.authType}
                    </TableCell>
                    <TableCell>
                      <Badge variant={connected ? "default" : "outline"}>
                        {connected ? "Connected" : "Not connected"}
                      </Badge>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
      </PageBody>
    </>
  );
}
