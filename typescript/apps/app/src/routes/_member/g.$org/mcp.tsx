import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { z } from "zod";
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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Textarea,
  toast,
} from "internal/components/ui";
import { ExternalLink, Plug, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { EmptyState, PageHeader } from "@/components/shell/PageHeader";
import { cn } from "@/lib/cn";
import { MCP_CONNECTORS, type McpConnector } from "@/lib/mcpConnectors";
import { groupQuery } from "@/lib/queries";
import {
  addMcpServer,
  connectMcpServer,
  disconnectMcpServer,
  listMcpServers,
  removeMcpServer,
  type McpServerView,
} from "@/server/fns/mcp";

// MCP servers: the tools the group's members reach through pear's MCP
// gateway. Admins (mcp.configure) add and remove servers; every member
// connects their own account to each one.

export const Route = createFileRoute("/_member/g/$org/mcp")({
  validateSearch: z.object({ server: z.string().optional() }),
  component: McpPage,
});

// The server's name doubles as its id and tool namespace.
const SERVER_NAME_PATTERN = /^[A-Za-z0-9_-]{1,64}$/;

const AUTH_LABELS: Record<string, string> = {
  oauth: "Each member signs in",
  token: "Each member adds a token",
  manual: "No sign-in needed",
};

function McpPage() {
  const { org } = Route.useParams();
  const { server: selectedId } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { data: group } = useQuery(groupQuery(org));
  const { data: servers = [], isPending } = useQuery({
    queryKey: ["mcp", org],
    queryFn: () => listMcpServers({ data: { org } }),
  });
  const canConfigure = group?.myActions.includes("mcp.configure") ?? false;
  const adding = selectedId === "new";
  const selected =
    servers.find((s) => s.server.id === selectedId) ??
    (adding ? null : servers[0]);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title="MCP servers"
        description="Tools your group's AI clients can use through Habitat"
      >
        {canConfigure && (
          <Button onClick={() => navigate({ search: { server: "new" } })}>
            <Plus data-icon="inline-start" />
            Add server
          </Button>
        )}
      </PageHeader>
      <div className="flex min-h-0 flex-1">
        <div className="flex w-80 shrink-0 flex-col gap-2 overflow-y-auto border-r p-3">
          {!isPending && servers.length === 0 && (
            <EmptyState icon={<Plug />} title="No MCP servers yet">
              {canConfigure
                ? "Add one to give members its tools."
                : "An admin can add servers here."}
            </EmptyState>
          )}
          {servers.map((s) => (
            <button
              key={s.server.id}
              type="button"
              onClick={() => navigate({ search: { server: s.server.id } })}
              className={cn(
                "flex items-center gap-3 rounded-lg border p-3 text-left",
                s.server.id === selected?.server.id && !adding
                  ? "border-primary bg-primary/5"
                  : "hover:bg-muted/60",
              )}
            >
              <span className="flex size-9 items-center justify-center rounded-lg bg-muted">
                <Plug className="size-4" />
              </span>
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{s.server.name}</p>
                <p className="truncate text-xs text-muted-foreground">
                  {AUTH_LABELS[s.server.authType] ?? s.server.authType}
                </p>
              </div>
              <Badge variant={s.connected ? "default" : "outline"}>
                {s.connected ? "Connected" : "Not connected"}
              </Badge>
            </button>
          ))}
        </div>
        <div className="min-w-0 flex-1 overflow-y-auto p-6">
          {adding && canConfigure ? (
            <AddServer
              org={org}
              onAdded={(id) => navigate({ search: { server: id } })}
            />
          ) : selected ? (
            <ServerDetail
              org={org}
              server={selected}
              canConfigure={canConfigure}
            />
          ) : null}
        </div>
      </div>
    </div>
  );
}

function ServerDetail({
  org,
  server: { server, connected },
  canConfigure,
}: {
  org: string;
  server: McpServerView;
  canConfigure: boolean;
}) {
  const queryClient = useQueryClient();
  const navigate = useNavigate({ from: Route.fullPath });
  const [token, setToken] = useState("");
  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["mcp", org] });
  const connect = useMutation({
    mutationFn: () =>
      connectMcpServer({
        data: { org, id: server.id, token: token || undefined },
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
        title: `Couldn't connect ${server.name}`,
        description: e.message,
      }),
  });
  const disconnect = useMutation({
    mutationFn: () => disconnectMcpServer({ data: { org, id: server.id } }),
    onSuccess: invalidate,
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't disconnect",
        description: e.message,
      }),
  });
  const remove = useMutation({
    mutationFn: () => removeMcpServer({ data: { org, id: server.id } }),
    onSuccess: async () => {
      await invalidate();
      await navigate({ search: {} });
    },
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't remove server",
        description: e.message,
      }),
  });
  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-6">
      <div className="flex items-start gap-3">
        <div className="flex-1">
          <h2 className="text-xl font-semibold">{server.name}</h2>
          {server.description && (
            <p className="text-sm text-muted-foreground">
              {server.description}
            </p>
          )}
        </div>
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
                    connect.isPending || (server.authType === "token" && !token)
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
    </div>
  );
}

// AddServer adds a known connector by name, or a custom server by URL.
function AddServer({
  org,
  onAdded,
}: {
  org: string;
  onAdded: (id: string) => void;
}) {
  const queryClient = useQueryClient();
  const [preset, setPreset] = useState<McpConnector | "custom" | null>(null);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [url, setUrl] = useState("");
  const [authType, setAuthType] = useState<"oauth" | "token" | "manual">(
    "oauth",
  );
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const add = useMutation({
    mutationFn: () =>
      addMcpServer({
        data:
          preset && preset !== "custom"
            ? {
                org,
                name,
                description: description || undefined,
                preset: preset.preset,
              }
            : {
                org,
                name,
                description: description || undefined,
                url,
                authType,
                clientId: clientId || undefined,
                clientSecret: clientSecret || undefined,
              },
      }),
    onSuccess: async (server) => {
      await queryClient.invalidateQueries({ queryKey: ["mcp", org] });
      onAdded(server.id);
    },
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't add server",
        description: e.message,
      }),
  });

  if (!preset) {
    return (
      <div className="mx-auto flex max-w-3xl flex-col gap-4">
        <h2 className="text-xl font-semibold">Add an MCP server</h2>
        <div className="grid gap-3 sm:grid-cols-2">
          {MCP_CONNECTORS.map((c) => (
            <button
              key={c.id}
              type="button"
              className="flex flex-col gap-1 rounded-xl border p-4 text-left hover:bg-muted/50"
              onClick={() => {
                setPreset(c);
                setName(c.defaultName);
                setDescription(c.description);
              }}
            >
              <span className="flex items-center gap-2 font-medium">
                <img src={`/connectors/${c.icon}`} alt="" className="size-4" />
                {c.name}
              </span>
              <span className="text-sm text-muted-foreground">
                {c.description}
              </span>
            </button>
          ))}
          <button
            type="button"
            className="flex flex-col gap-1 rounded-xl border border-dashed p-4 text-left hover:bg-muted/50"
            onClick={() => setPreset("custom")}
          >
            <span className="font-medium">Custom server</span>
            <span className="text-sm text-muted-foreground">
              Any streamable HTTP MCP server, by URL.
            </span>
          </button>
        </div>
      </div>
    );
  }

  const nameOk = SERVER_NAME_PATTERN.test(name);
  return (
    <form
      className="mx-auto flex max-w-xl flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        add.mutate();
      }}
    >
      <div className="flex items-center gap-2">
        <h2 className="flex-1 text-xl font-semibold">
          {preset === "custom" ? "Custom server" : preset.name}
        </h2>
        {preset !== "custom" && (
          <Button
            variant="ghost"
            size="sm"
            render={
              <a href={preset.docsUrl} target="_blank" rel="noreferrer" />
            }
          >
            <ExternalLink data-icon="inline-start" />
            Docs
          </Button>
        )}
      </div>
      <Field>
        <FieldLabel htmlFor="mcp-name">Name</FieldLabel>
        <Input
          id="mcp-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          aria-invalid={!!name && !nameOk}
        />
        <FieldDescription>
          Letters, digits, dashes and underscores. It's also the server's tool
          namespace.
        </FieldDescription>
      </Field>
      <Field>
        <FieldLabel htmlFor="mcp-description">Description</FieldLabel>
        <Textarea
          id="mcp-description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
      </Field>
      {preset === "custom" && (
        <>
          <Field>
            <FieldLabel htmlFor="mcp-url">URL</FieldLabel>
            <Input
              id="mcp-url"
              placeholder="https://example.com/mcp"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel>Sign-in</FieldLabel>
            <Select
              value={authType}
              onValueChange={(v) => setAuthType(v as typeof authType)}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="oauth">
                  {AUTH_LABELS.oauth} (OAuth)
                </SelectItem>
                <SelectItem value="token">{AUTH_LABELS.token}</SelectItem>
                <SelectItem value="manual">{AUTH_LABELS.manual}</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          {authType === "oauth" && (
            <>
              <Field>
                <FieldLabel htmlFor="mcp-client-id">
                  OAuth client id (optional)
                </FieldLabel>
                <Input
                  id="mcp-client-id"
                  value={clientId}
                  onChange={(e) => setClientId(e.target.value)}
                />
                <FieldDescription>
                  Leave empty if the server supports dynamic client
                  registration.
                </FieldDescription>
              </Field>
              <Field>
                <FieldLabel htmlFor="mcp-client-secret">
                  OAuth client secret (optional)
                </FieldLabel>
                <Input
                  id="mcp-client-secret"
                  type="password"
                  value={clientSecret}
                  onChange={(e) => setClientSecret(e.target.value)}
                />
              </Field>
            </>
          )}
        </>
      )}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" onClick={() => setPreset(null)}>
          Back
        </Button>
        <Button
          type="submit"
          disabled={!nameOk || (preset === "custom" && !url) || add.isPending}
        >
          Add server
        </Button>
      </div>
    </form>
  );
}
