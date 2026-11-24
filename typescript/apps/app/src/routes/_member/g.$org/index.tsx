import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import {
  Button,
  Card,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  Item,
  ItemContent,
  ItemMedia,
  ItemTitle,
  toast,
} from "internal/components/ui";
import {
  ArrowRight,
  ChevronRight,
  CircleCheck,
  CircleDashed,
  Plug,
} from "lucide-react";
import { useState } from "react";
import { cn } from "@/lib/cn";
import { mcpServersQuery } from "@/lib/queries";
import { PageBody, PageHeader } from "@/components/shell/PageHeader";
import { connectMcpServer, type McpServerView } from "@/server/fns/mcp";

// The org's index: Home — the group's AI-facing servers its members
// haven't connected yet.

export const Route = createFileRoute("/_member/g/$org/")({
  loader: ({ context, params }) =>
    context.queryClient.query(mcpServersQuery(params.org)),
  component: HomePage,
});

function HomePage() {
  const { org } = Route.useParams();

  const queryClient = useQueryClient();
  const { data: servers } = useSuspenseQuery(mcpServersQuery(org));
  const unconnected = servers.filter((s) => !s.connected);
  const allConnected = servers.length > 0 && unconnected.length === 0;
  // Controlled state: the collapse is open when the user last toggled it,
  // or by default when servers are pending or unconnected, and closed when
  // everything is connected.
  const [userOpen, setUserOpen] = useState<boolean | null>(null);
  const open = userOpen ?? !allConnected;
  const connect = useMutation({
    mutationFn: (server: McpServerView["server"]) =>
      connectMcpServer({ data: { org, id: server.id } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["mcp", org] }),
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't connect server",
        description: e.message,
      }),
  });

  return (
    <>
      <PageHeader
        title="Home"
        description="Connect your group's AI clients to its servers."
      />
      <PageBody className="flex max-w-3xl flex-col gap-6">
        <Card size="sm">
          <Collapsible open={open} onOpenChange={setUserOpen} className="px-2">
            <CollapsibleTrigger
              render={
                <Button variant="ghost" className="justify-between w-full">
                  Connections
                  <span className="flex items-center gap-2">
                    {allConnected && servers.length > 1 && (
                      <CircleCheck className="size-4 text-emerald-600" />
                    )}
                    <ChevronRight
                      className={cn(
                        "size-4 transition-transform",
                        open ? "rotate-90" : "",
                      )}
                    />
                  </span>
                </Button>
              }
            ></CollapsibleTrigger>
            <CollapsibleContent className="py-1">
              {unconnected.length > 0 ? (
                <div className="flex gap-3 overflow-x-auto">
                  {servers.map((s) => (
                    <ServerItem
                      key={s.server.id}
                      server={s.server}
                      connected={s.connected}
                      onConnect={() => connect.mutate(s.server)}
                    />
                  ))}
                </div>
              ) : servers.length === 0 ? (
                <div className="flex p-2 items-center flex-col gap-2">
                  <span className="text-muted-foreground">
                    No connections for this organization
                  </span>
                  <Button render={<Link to="/g/$org/mcp" params={{ org }} />}>
                    Add MCP servers
                  </Button>
                </div>
              ) : null}
              {servers.length > 0 && (
                <Button
                  variant="link"
                  render={<Link to="/g/$org/mcp" params={{ org }} />}
                  size="sm"
                  className="ml-auto"
                >
                  MCP servers
                  <ArrowRight data-icon="inline-end" />
                </Button>
              )}
            </CollapsibleContent>
          </Collapsible>
        </Card>
      </PageBody>
    </>
  );
}

// ServerItem is one server in the horizontal scroll: a Plugin icon, the
// server's name, and a circle — dashed while it's pending, checked once
// connected. Clicking a pending server starts connecting it.
function ServerItem({
  server,
  connected,
  onConnect,
}: {
  server: McpServerView["server"];
  connected: boolean;
  onConnect: () => void;
}) {
  return (
    <Item
      variant="outline"
      size="xs"
      render={connected ? undefined : <button type="button" />}
      onClick={connected ? undefined : onConnect}
      className={cn(
        "w-auto shrink-0",
        connected ? "cursor-default" : "cursor-pointer",
      )}
    >
      <ItemMedia>
        <Plug className="size-4" />
      </ItemMedia>
      <ItemContent>
        <ItemTitle>{server.name}</ItemTitle>
      </ItemContent>
      <ItemMedia>
        {connected ? (
          <CircleCheck className="size-4 text-emerald-600" />
        ) : (
          <CircleDashed className="size-4 text-muted-foreground" />
        )}
      </ItemMedia>
    </Item>
  );
}
