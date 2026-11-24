import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import {
  Button,
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
import { ArrowLeft, ExternalLink } from "lucide-react";
import { useState } from "react";
import { PageBody, PageHeader } from "@/components/shell/PageHeader";
import {
  AUTH_LABELS,
  MCP_CONNECTORS,
  type McpConnector,
} from "@/lib/mcpConnectors";
import { groupQuery } from "@/lib/queries";
import { addMcpServer } from "@/server/fns/mcp";

// Add an MCP server: a known connector by name, or a custom server by URL.
// Needs mcp.configure.

export const Route = createFileRoute("/_member/g/$org/mcp/new")({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(groupQuery(params.org)),
  component: NewServerPage,
});

// The server's name doubles as its id and tool namespace.
const SERVER_NAME_PATTERN = /^[A-Za-z0-9_-]{1,64}$/;

function NewServerPage() {
  const { org } = Route.useParams();
  const { data: group } = useSuspenseQuery(groupQuery(org));
  const canConfigure = group.myActions.includes("mcp.configure");
  return (
    <>
      <PageHeader title="Add MCP server">
        <Button
          variant="ghost"
          render={<Link to="/g/$org/mcp" params={{ org }} />}
        >
          <ArrowLeft data-icon="inline-start" />
          MCP servers
        </Button>
      </PageHeader>
      <PageBody>
        {canConfigure ? (
          <AddServer />
        ) : (
          <p className="text-sm text-muted-foreground">
            Only admins can add servers.
          </p>
        )}
      </PageBody>
    </>
  );
}

// AddServer adds a known connector by name, or a custom server by URL.
function AddServer() {
  const { org } = Route.useParams();
  const navigate = useNavigate();
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
      await navigate({
        to: "/g/$org/mcp/$serverId",
        params: { org, serverId: server.id },
      });
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
      <div className="flex max-w-3xl flex-col gap-4">
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
      className="flex max-w-xl flex-col gap-4"
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
