import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Field,
  FieldDescription,
  FieldLabel,
  Input,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  toast,
} from "internal/components/ui";
import { ArrowLeft, Trash2 } from "lucide-react";
import { Suspense, useState } from "react";
import { McpServerIcon } from "@/components/mcp/McpServerIcon";
import { PageBody, PageHeader } from "@/components/shell/PageHeader";
import { AUTH_LABELS, mcpServerDisplay } from "@/lib/mcpConnectors";
import { groupQuery, mcpServersQuery, mcpToolsQuery } from "@/lib/queries";
import {
  connectMcpServer,
  disconnectMcpServer,
  removeMcpServer,
} from "@/server/fns/mcp";

// One MCP server: its details, the member's own connection to it, and the
// tools it offers.

export const Route = createFileRoute("/_member/g/$org/mcp/$serverId")({
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(groupQuery(params.org)),
      context.queryClient.ensureQueryData(mcpServersQuery(params.org)),
    ]),
  component: ServerPage,
});

function ServerPage() {
  const { org, serverId } = Route.useParams();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { data: group } = useSuspenseQuery(groupQuery(org));
  const { data: servers } = useSuspenseQuery(mcpServersQuery(org));
  const [token, setToken] = useState("");
  const found = servers.find((s) => s.server.id === serverId);
  const canConfigure = group.myActions.includes("mcp.configure");
  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["mcp", org] });
  const name = found ? mcpServerDisplay(found.server).name : serverId;

  const connect = useMutation({
    mutationFn: () =>
      connectMcpServer({
        data: { org, id: serverId, token: token || undefined },
      }),
    onSuccess: async ({ authorizationUrl }) => {
      if (authorizationUrl) {
        window.location.href = authorizationUrl;
        return;
      }
      setToken("");
      await invalidate();
    },
    onError: (e) =>
      toast.add({
        type: "error",
        title: `Couldn't connect ${name}`,
        description: e.message,
      }),
  });
  const disconnect = useMutation({
    mutationFn: () => disconnectMcpServer({ data: { org, id: serverId } }),
    onSuccess: invalidate,
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't disconnect",
        description: e.message,
      }),
  });
  const remove = useMutation({
    mutationFn: () => removeMcpServer({ data: { org, id: serverId } }),
    onSuccess: async () => {
      await invalidate();
      await navigate({ to: "/g/$org/mcp", params: { org } });
    },
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't remove server",
        description: e.message,
      }),
  });

  const back = (
    <Button variant="ghost" render={<Link to="/g/$org/mcp" params={{ org }} />}>
      <ArrowLeft data-icon="inline-start" />
      MCP servers
    </Button>
  );
  if (!found) {
    return (
      <>
        <PageHeader title="Server not found">{back}</PageHeader>
        <PageBody>
          <p className="text-sm text-muted-foreground">
            This group has no MCP server called {serverId}.
          </p>
        </PageBody>
      </>
    );
  }
  const { server, connected } = found;
  const display = mcpServerDisplay(server);

  return (
    <>
      <PageHeader title={display.name} description={server.description}>
        {back}
        {canConfigure && (
          <Button
            variant="destructive"
            disabled={remove.isPending}
            onClick={() => remove.mutate()}
          >
            <Trash2 data-icon="inline-start" />
            Remove
          </Button>
        )}
      </PageHeader>
      <PageBody className="flex max-w-3xl flex-col gap-6">
        <div className="flex items-center gap-3">
          <McpServerIcon icon={display.icon} className="size-10" />
          <Badge variant={connected ? "default" : "outline"}>
            {connected ? "Connected" : "Not connected"}
          </Badge>
        </div>
        <Card>
          <CardHeader>
            <CardTitle>Your connection</CardTitle>
            <CardDescription>
              {server.authType === "manual"
                ? "This server needs no sign-in, so every member is connected."
                : connected
                  ? "You're connected. Your AI clients can use this server's tools through Habitat."
                  : server.authType === "token"
                    ? "Add your own access token to use this server."
                    : "Sign in with your own account to use this server."}
            </CardDescription>
          </CardHeader>
          {server.authType !== "manual" && (
            <CardContent className="flex flex-col gap-3">
              {!connected && server.authType === "token" && (
                <Field>
                  <FieldLabel htmlFor="mcp-token">Access token</FieldLabel>
                  <Input
                    id="mcp-token"
                    type="password"
                    value={token}
                    onChange={(e) => setToken(e.target.value)}
                  />
                  <FieldDescription>
                    Stored encrypted and never shown again.
                  </FieldDescription>
                </Field>
              )}
              <div className="flex gap-2">
                {connected ? (
                  <Button
                    variant="outline"
                    disabled={disconnect.isPending}
                    onClick={() => disconnect.mutate()}
                  >
                    Disconnect
                  </Button>
                ) : (
                  <Button
                    disabled={
                      connect.isPending ||
                      (server.authType === "token" && !token)
                    }
                    onClick={() => connect.mutate()}
                  >
                    {server.authType === "token" ? "Save token" : "Connect"}
                  </Button>
                )}
              </div>
            </CardContent>
          )}
        </Card>
        <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
          <dt className="text-muted-foreground">Id</dt>
          <dd className="font-mono">{server.id}</dd>
          <dt className="text-muted-foreground">Sign-in</dt>
          <dd>{AUTH_LABELS[server.authType] ?? server.authType}</dd>
          {server.url && (
            <>
              <dt className="text-muted-foreground">URL</dt>
              <dd className="break-all font-mono">{server.url}</dd>
            </>
          )}
        </dl>
        <section className="flex flex-col gap-2">
          <h2 className="text-base font-semibold">Tools</h2>
          {connected ? (
            <Suspense
              fallback={
                <p className="text-sm text-muted-foreground">Loading tools…</p>
              }
            >
              <ToolsTable org={org} serverId={serverId} />
            </Suspense>
          ) : (
            <p className="text-sm text-muted-foreground">
              Connect to {display.name} to see its tools.
            </p>
          )}
        </section>
      </PageBody>
    </>
  );
}

function ToolsTable({ org, serverId }: { org: string; serverId: string }) {
  const { data } = useSuspenseQuery(mcpToolsQuery(org, serverId));
  if (data.error) {
    return (
      <p className="text-sm text-destructive">
        Couldn't list tools: {data.error}
      </p>
    );
  }
  if (data.tools.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        This server offers no tools.
      </p>
    );
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Tool</TableHead>
          <TableHead>Description</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {data.tools.map((t) => (
          <TableRow key={t.name}>
            <TableCell className="align-top">
              <p className="font-medium">{t.title ?? t.name}</p>
              {t.title && (
                <p className="font-mono text-xs text-muted-foreground">
                  {t.name}
                </p>
              )}
            </TableCell>
            <TableCell className="whitespace-normal text-muted-foreground">
              {t.description}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
