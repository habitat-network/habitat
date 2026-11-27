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
import { Controller, useForm } from "react-hook-form";
import { PageBody, PageHeader } from "@/components/shell/PageHeader";
import {
  AUTH_LABELS,
  MCP_CONNECTORS,
  type McpConnector,
  serverUrlError,
} from "@/lib/mcpConnectors";
import { groupQuery } from "@/lib/queries/groups";
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

type AuthType = "oauth" | "token" | "manual";
type AddServerValues = {
  name: string;
  description: string;
  url: string;
  authType: AuthType;
  clientId: string;
  clientSecret: string;
};
const EMPTY: AddServerValues = {
  name: "",
  description: "",
  url: "",
  authType: "oauth",
  clientId: "",
  clientSecret: "",
};

// AddServer adds a known connector by name, or a custom server by URL.
function AddServer() {
  const { org } = Route.useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [preset, setPreset] = useState<McpConnector | "custom" | null>(null);
  const {
    register,
    control,
    handleSubmit,
    reset,
    watch,
    formState: { errors, isValid },
  } = useForm<AddServerValues>({ mode: "onChange", defaultValues: EMPTY });
  const authType = watch("authType");
  const url = watch("url");
  const add = useMutation({
    mutationFn: ({
      name,
      description,
      url,
      authType,
      clientId,
      clientSecret,
    }: AddServerValues) =>
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
                url: url.trim(),
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
                reset({
                  ...EMPTY,
                  name: c.defaultName,
                  description: c.description,
                });
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

  const urlError = url ? serverUrlError(url) : null;
  return (
    <form
      className="flex max-w-xl flex-col gap-4"
      onSubmit={handleSubmit((v) => add.mutate(v))}
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
          aria-invalid={!!errors.name}
          {...register("name", {
            validate: (v) => SERVER_NAME_PATTERN.test(v),
          })}
        />
        <FieldDescription>
          Letters, digits, dashes and underscores. It's also the server's tool
          namespace.
        </FieldDescription>
      </Field>
      <Field>
        <FieldLabel htmlFor="mcp-description">Description</FieldLabel>
        <Textarea id="mcp-description" {...register("description")} />
      </Field>
      {preset === "custom" && (
        <>
          <Field>
            <FieldLabel htmlFor="mcp-url">URL</FieldLabel>
            <Input
              id="mcp-url"
              placeholder="https://example.com/mcp"
              {...register("url", { required: preset === "custom" })}
              aria-invalid={!!urlError}
            />
            {urlError && (
              <FieldDescription className="text-destructive">
                {urlError}
              </FieldDescription>
            )}
          </Field>
          <Field>
            <FieldLabel>Sign-in</FieldLabel>
            <Controller
              control={control}
              name="authType"
              render={({ field }) => (
                <Select
                  value={field.value}
                  onValueChange={(v) => field.onChange(v as AuthType)}
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
              )}
            />
          </Field>
          {authType === "oauth" && (
            <>
              <Field>
                <FieldLabel htmlFor="mcp-client-id">
                  OAuth client id (optional)
                </FieldLabel>
                <Input id="mcp-client-id" {...register("clientId")} />
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
                  {...register("clientSecret")}
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
          disabled={
            !isValid || (preset === "custom" && !!urlError) || add.isPending
          }
        >
          Add server
        </Button>
      </div>
    </form>
  );
}
