import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import {
  Badge,
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
  Spinner,
} from "internal/components/ui";
import {
  McpTokenDialog,
  useMcpConnect,
  type McpServerWithStatus,
} from "internal/mcp";
import { CheckCircle2Icon, PlugIcon } from "lucide-react";
import { listOrgMcpServers, startMcpConnect } from "@/server/functions";
import { useOrgStore } from "@/stores/org";

export const Route = createFileRoute("/_member/")({
  component: Home,
});

const serversKey = (org: string) => ["mcp-servers", org] as const;

function Home() {
  const org = useOrgStore((s) => s.selectedOrg);
  const { data: servers, isPending } = useQuery({
    queryKey: serversKey(org ?? ""),
    queryFn: () => listOrgMcpServers({ data: { org: org! } }),
    enabled: !!org,
  });

  // A manual server is shared by the whole org, so there's nothing for the
  // member to connect.
  const connected = (servers ?? []).filter((s) => s.connected);
  const available = (servers ?? []).filter((s) => !s.connected);

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-1 flex-col gap-8 p-6">
      <p className="text-muted-foreground">
        Start a new chat from the sidebar.
      </p>
      {isPending ? (
        <Spinner />
      ) : servers && servers.length > 0 ? (
        <>
          <ServerGroup
            title="Connected"
            empty="You haven't connected any servers yet."
            servers={connected}
            org={org!}
          />
          {available.length > 0 && (
            <ServerGroup title="Not connected" servers={available} org={org!} />
          )}
        </>
      ) : null}
    </div>
  );
}

function ServerGroup({
  title,
  empty,
  servers,
  org,
}: {
  title: string;
  empty?: string;
  servers: McpServerWithStatus[];
  org: string;
}) {
  return (
    <section className="flex flex-col gap-3">
      <h2 className="text-sm font-semibold">
        {title}{" "}
        <span className="font-normal text-muted-foreground">
          {servers.length}
        </span>
      </h2>
      {servers.length === 0 ? (
        <p className="text-sm text-muted-foreground">{empty}</p>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {servers.map((s) => (
            <ServerCard key={s.server.id} org={org} status={s} />
          ))}
        </div>
      )}
    </section>
  );
}

// ServerCard is one MCP server. A connected one just says so; one the member
// hasn't connected is a button that starts connecting: an OAuth popup, or a
// token dialog for token servers.
function ServerCard({
  org,
  status: { server, connected },
}: {
  org: string;
  status: McpServerWithStatus;
}) {
  const queryClient = useQueryClient();
  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: serversKey(org) });
  const { connect, connecting, error } = useMcpConnect({
    start: (id) => startMcpConnect({ data: { org, id } }),
    onSettled: () => void refresh(),
  });

  const body = (
    <>
      <CardHeader>
        <div className="flex items-center justify-between gap-2">
          <CardTitle className="truncate">{server.name}</CardTitle>
          {connected ? (
            <Badge variant="secondary" className="gap-1">
              <CheckCircle2Icon className="size-3" />
              {server.authType === "manual" ? "Shared" : "Connected"}
            </Badge>
          ) : (
            <Badge variant="outline" className="gap-1">
              <PlugIcon className="size-3" />
              {connecting ? "Connecting…" : "Connect"}
            </Badge>
          )}
        </div>
        <CardDescription className="line-clamp-2">
          {server.description}
        </CardDescription>
        {error && <p className="text-xs text-destructive">{error.message}</p>}
      </CardHeader>
    </>
  );

  if (connected) return <Card>{body}</Card>;

  if (server.authType === "token") {
    return (
      <Card>
        {body}
        <div className="px-4 pb-4">
          <McpTokenDialog
            serverName={server.name}
            save={async (token) => {
              await startMcpConnect({ data: { org, id: server.id, token } });
              await refresh();
            }}
          />
        </div>
      </Card>
    );
  }

  return (
    <Card
      role="button"
      tabIndex={0}
      aria-disabled={connecting}
      className="cursor-pointer transition-colors hover:bg-muted/50"
      onClick={() => !connecting && connect(server.id)}
      onKeyDown={(e) => {
        if ((e.key === "Enter" || e.key === " ") && !connecting) {
          e.preventDefault();
          connect(server.id);
        }
      }}
    >
      {body}
    </Card>
  );
}
