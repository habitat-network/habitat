import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Controller, useForm, useWatch } from "react-hook-form";
import type { AuthManager } from "internal";
import {
  McpTokenDialog,
  startMcpAuthorization,
  useMcpConnect,
} from "internal/mcp";
import { pearAgent } from "@/queries/pearAgent";
import type { DidString } from "@atproto/lex";
import {
  addMcpServerMutationOptions,
  disconnectMcpServerMutationOptions,
  mcpServersQueryKey,
  removeMcpServerMutationOptions,
  type McpServerWithStatus,
} from "@/queries/mcp";
import { MCP_CONNECTORS, type McpConnector } from "@/lib/mcpConnectors";
import {
  Badge,
  Button,
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldLabel,
  FieldTitle,
  Input,
  RadioGroup,
  RadioGroupItem,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  toast,
} from "internal/components/ui";

// Mirrors mcpgateway.serverNamePattern (Go). The name doubles as the
// server's ID and, once connected, the namespace its tools are
// exposed under (e.g. "cloudflare:docs"), so it's restricted to a plain,
// unique-per-org slug.
const SERVER_NAME_PATTERN = /^[A-Za-z0-9_-]{1,64}$/;

// How pear authenticates to a server; see mcpgateway.AuthType (Go).
type AuthType = "oauth" | "manual";

interface AddServerFormValues {
  name: string;
  description: string;
  authType: AuthType;
  url: string;
  // A client pre-registered with the server's authorization server, for
  // oauth servers that don't support dynamic client registration.
  clientId: string;
  clientSecret: string;
}

interface ConnectorFormValues {
  name: string;
}

// pear's MCP OAuth callback (pearserver.MCPOAuthCallbackPath in Go), which
// admins register as the redirect URI of a pre-registered client.
const OAUTH_CALLBACK_URL = `https://${import.meta.env.VITE_HABITAT_DOMAIN}/mcp-oauth/callback`;

// isUri narrows s to the lexicon's uri string format.
function isUri(s: string): s is `${string}:${string}` {
  return URL.canParse(s);
}

export function McpServersEditor({
  org,
  servers,
  canConfigureMcp,
  authManager,
}: {
  org: DidString;
  servers: McpServerWithStatus[];
  canConfigureMcp: boolean;
  authManager: AuthManager;
}) {
  return (
    <div className="flex flex-col gap-6">
      <OrgMcpUrl org={org} />
      <div className="flex flex-col gap-4">
        <div className="flex items-center justify-between">
          <h2 className="text-base font-semibold">
            MCP servers ({servers.length})
          </h2>
          {canConfigureMcp && (
            <AddServerDialog org={org} authManager={authManager} />
          )}
        </div>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Description</TableHead>
              <TableHead>Status</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {servers.map(({ server, connected }) => (
              <TableRow key={server.id}>
                <TableCell className="font-medium">{server.name}</TableCell>
                <TableCell className="text-muted-foreground">
                  {server.description}
                </TableCell>
                <TableCell>
                  <ConnectionCell
                    org={org}
                    server={server}
                    connected={connected}
                    authManager={authManager}
                  />
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex items-center justify-end gap-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      render={
                        <Link
                          to="/orgs/$org/settings/mcp-debug"
                          params={{ org }}
                          search={{ server: server.id }}
                        />
                      }
                    >
                      Debug
                    </Button>
                    {canConfigureMcp && (
                      <RemoveServerButton
                        org={org}
                        id={server.id}
                        name={server.name}
                        authManager={authManager}
                      />
                    )}
                  </div>
                </TableCell>
              </TableRow>
            ))}
            {servers.length === 0 && (
              <TableRow>
                <TableCell colSpan={4} className="text-muted-foreground">
                  No MCP servers configured yet.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}

// orgMcpUrl is the org's MCP endpoint (mcpserver.Path + "/" + org in Go),
// served from the API origin. The org is identified by DID since the
// frontend has no org handle to prefer.
function orgMcpUrl(org: DidString) {
  return `https://${import.meta.env.VITE_HABITAT_DOMAIN}/mcp/${org}`;
}

// OrgMcpUrl shows the org's MCP URL with a copy button, for adding to MCP
// clients.
function OrgMcpUrl({ org }: { org: DidString }) {
  const url = orgMcpUrl(org);
  return (
    <div className="flex flex-col gap-2">
      <h2 className="text-base font-semibold">MCP URL</h2>
      <p className="text-sm text-muted-foreground">
        Add this URL to your MCP client to use this org&apos;s data and
        connected servers.
      </p>
      <div className="flex gap-2">
        <Input readOnly value={url} className="font-mono" />
        <Button
          variant="outline"
          onClick={async () => {
            try {
              await navigator.clipboard.writeText(url);
              toast.add({ type: "success", title: "Copied MCP URL" });
            } catch {
              toast.add({ type: "error", title: "Failed to copy MCP URL" });
            }
          }}
        >
          Copy
        </Button>
      </div>
    </div>
  );
}

function ConnectionCell({
  org,
  server,
  connected,
  authManager,
}: {
  org: DidString;
  server: McpServerWithStatus["server"];
  connected: boolean;
  authManager: AuthManager;
}) {
  const queryClient = useQueryClient();
  const {
    connect,
    connecting,
    error: startError,
  } = useMcpConnect({
    start: (id) =>
      startMcpAuthorization(pearAgent(authManager, `${org}#habitat`), org, id),
    onSettled: () =>
      void queryClient.invalidateQueries({ queryKey: mcpServersQueryKey(org) }),
  });

  const {
    mutate: disconnect,
    isPending: disconnecting,
    error: disconnectError,
  } = useMutation(disconnectMcpServerMutationOptions(authManager, org));

  if (server.authType === "manual") {
    return <Badge variant="secondary">Shared</Badge>;
  }

  if (connected) {
    return (
      <div className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          <Badge variant="secondary">Connected</Badge>
          <Button
            variant="ghost"
            size="sm"
            disabled={disconnecting}
            onClick={() => disconnect(server.id)}
          >
            {disconnecting ? "Disconnecting…" : "Disconnect"}
          </Button>
        </div>
        <FieldError
          errors={disconnectError ? [{ message: disconnectError.message }] : []}
        />
      </div>
    );
  }

  if (server.authType === "token") {
    return (
      <McpTokenDialog
        serverName={server.name}
        save={async (token) => {
          await startMcpAuthorization(
            pearAgent(authManager, `${org}#habitat`),
            org,
            server.id,
            token,
          );
          await queryClient.invalidateQueries({
            queryKey: mcpServersQueryKey(org),
          });
        }}
      />
    );
  }

  return (
    <div className="flex flex-col gap-1">
      <Button
        variant="outline"
        size="sm"
        className="self-start"
        disabled={connecting}
        onClick={() => connect(server.id)}
      >
        {connecting ? "Connecting…" : "Connect"}
      </Button>
      <FieldError
        errors={startError ? [{ message: startError.message }] : []}
      />
    </div>
  );
}

// AddServerDialog first asks which connector to add, then shows the form for
// it: a connector-specific one for a known connector (see mcpConnectors.ts), or
// the generic one for a custom server.
function AddServerDialog({
  org,
  authManager,
}: {
  org: DidString;
  authManager: AuthManager;
}) {
  const [open, setOpen] = useState(false);
  const [choice, setChoice] = useState<McpConnector | "custom" | null>(null);

  const close = () => {
    setOpen(false);
    setChoice(null);
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setChoice(null);
      }}
    >
      <DialogTrigger render={<Button size="sm" />}>Add server</DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {choice === null
              ? "Add an MCP server"
              : choice === "custom"
                ? "Add a custom MCP server"
                : `Connect ${choice.name}`}
          </DialogTitle>
        </DialogHeader>
        {choice === null ? (
          <ConnectorPicker onSelect={setChoice} />
        ) : choice === "custom" ? (
          <CustomServerForm
            org={org}
            authManager={authManager}
            onBack={() => setChoice(null)}
            onDone={close}
          />
        ) : (
          <ConnectorForm
            connector={choice}
            org={org}
            authManager={authManager}
            onBack={() => setChoice(null)}
            onDone={close}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function ConnectorPicker({
  onSelect,
}: {
  onSelect: (choice: McpConnector | "custom") => void;
}) {
  return (
    <div className="flex flex-col gap-2">
      {MCP_CONNECTORS.map((connector) => (
        <Button
          key={connector.id}
          variant="outline"
          className="h-auto justify-start py-3 text-left"
          onClick={() => onSelect(connector)}
        >
          <div className="flex flex-col gap-1">
            <span className="font-medium">{connector.name}</span>
            <span className="text-xs font-normal text-muted-foreground">
              {connector.description}
            </span>
          </div>
        </Button>
      ))}
      <Button
        variant="outline"
        className="h-auto justify-start py-3 text-left"
        onClick={() => onSelect("custom")}
      >
        <div className="flex flex-col gap-1">
          <span className="font-medium">Custom server</span>
          <span className="text-xs font-normal text-muted-foreground">
            Connect any MCP server by URL.
          </span>
        </div>
      </Button>
    </div>
  );
}

// ConnectorForm asks only for a name; pear fills in the rest from the
// connector's preset.
function ConnectorForm({
  connector,
  org,
  authManager,
  onBack,
  onDone,
}: {
  connector: McpConnector;
  org: DidString;
  authManager: AuthManager;
  onBack: () => void;
  onDone: () => void;
}) {
  const {
    register,
    handleSubmit,
    formState: { errors, isValid },
  } = useForm<ConnectorFormValues>({
    mode: "onChange",
    defaultValues: { name: connector.defaultName },
  });

  const {
    mutate: add,
    isPending: adding,
    error,
  } = useMutation(addMcpServerMutationOptions(authManager, org));

  const submit = handleSubmit(({ name }) => {
    add(
      {
        name,
        description: connector.description,
        preset: connector.preset,
      },
      { onSuccess: onDone },
    );
  });

  return (
    <form className="flex flex-col gap-4" onSubmit={submit}>
      <p className="text-sm text-muted-foreground">
        {connector.description}{" "}
        {connector.memberAuth === "token"
          ? `Each member adds their own ${connector.name} access token afterward.`
          : `Each member signs in with their own ${connector.name} account afterward.`}{" "}
        <a
          href={connector.docsUrl}
          target="_blank"
          rel="noreferrer"
          className="underline"
        >
          Learn more
        </a>
      </p>
      <Field>
        <FieldLabel htmlFor="mcp-name">Name</FieldLabel>
        <Input
          id="mcp-name"
          {...register("name", {
            required: "Enter a name.",
            pattern: {
              value: SERVER_NAME_PATTERN,
              message: "Use 1-64 letters, numbers, hyphens, or underscores.",
            },
          })}
        />
        <FieldError errors={[errors.name]} />
        <p className="text-xs text-muted-foreground">
          Unique within this community, and can't be changed later.
        </p>
      </Field>
      <FieldError errors={error ? [{ message: error.message }] : []} />
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={onBack}>
          Back
        </Button>
        <Button type="submit" disabled={adding || !isValid}>
          {adding ? "Adding…" : `Add ${connector.name}`}
        </Button>
      </DialogFooter>
    </form>
  );
}

// CustomServerForm is the generic form for any MCP server by URL.
function CustomServerForm({
  org,
  authManager,
  onBack,
  onDone,
}: {
  org: DidString;
  authManager: AuthManager;
  onBack: () => void;
  onDone: () => void;
}) {
  const {
    register,
    handleSubmit,
    control,
    formState: { errors, isValid },
  } = useForm<AddServerFormValues>({
    mode: "onChange",
    defaultValues: {
      name: "",
      description: "",
      authType: "oauth",
      url: "",
      clientId: "",
      clientSecret: "",
    },
  });
  const authType = useWatch({ control, name: "authType" });

  const {
    mutate: add,
    isPending: adding,
    error,
  } = useMutation(addMcpServerMutationOptions(authManager, org));

  // Adding a server registers pear with its authorization server, if it has
  // one, but nobody signs in here. For an oauth server, members (including
  // this admin) connect afterward with the "Connect" button.
  const submit = handleSubmit(
    ({ name, description, authType, url, clientId, clientSecret }) => {
      if (!isUri(url)) return;
      const oauth = authType === "oauth";
      add(
        {
          name,
          description: description || undefined,
          url,
          authType,
          clientId: (oauth && clientId) || undefined,
          clientSecret: (oauth && clientId && clientSecret) || undefined,
        },
        { onSuccess: onDone },
      );
    },
  );

  return (
    <form className="flex flex-col gap-4" onSubmit={submit}>
      <Field>
        <FieldLabel htmlFor="mcp-name">Name</FieldLabel>
        <Input
          id="mcp-name"
          placeholder="cloudflare"
          autoFocus
          {...register("name", {
            required: "Enter a name.",
            pattern: {
              value: SERVER_NAME_PATTERN,
              message: "Use 1-64 letters, numbers, hyphens, or underscores.",
            },
          })}
        />
        <FieldError errors={[errors.name]} />
        <p className="text-xs text-muted-foreground">
          Letters, numbers, hyphens, and underscores only. Unique within this
          community, and can't be changed later.
        </p>
      </Field>
      <Field>
        <FieldLabel htmlFor="mcp-description">
          Description (optional)
        </FieldLabel>
        <Input id="mcp-description" {...register("description")} />
      </Field>
      <Field>
        <FieldLabel>How members connect</FieldLabel>
        <Controller
          control={control}
          name="authType"
          render={({ field: { value, onChange } }) => (
            <RadioGroup
              value={value}
              onValueChange={(next) =>
                onChange(next === "manual" ? "manual" : "oauth")
              }
            >
              <FieldLabel htmlFor="mcp-auth-oauth">
                <Field orientation="horizontal">
                  <FieldContent>
                    <FieldTitle>Sign in with OAuth</FieldTitle>
                    <FieldDescription>
                      Each member signs in with their own account. Use this for
                      servers that support MCP sign-in.
                    </FieldDescription>
                  </FieldContent>
                  <RadioGroupItem value="oauth" id="mcp-auth-oauth" />
                </Field>
              </FieldLabel>
              <FieldLabel htmlFor="mcp-auth-manual">
                <Field orientation="horizontal">
                  <FieldContent>
                    <FieldTitle>No auth</FieldTitle>
                    <FieldDescription>
                      Every member connects to it automatically. Use this for
                      servers that don't require sign-in.
                    </FieldDescription>
                  </FieldContent>
                  <RadioGroupItem value="manual" id="mcp-auth-manual" />
                </Field>
              </FieldLabel>
            </RadioGroup>
          )}
        />
      </Field>
      <Field>
        <FieldLabel htmlFor="mcp-url">Server URL</FieldLabel>
        <Input
          id="mcp-url"
          type="url"
          placeholder="https://mcp.example.com/mcp"
          {...register("url", {
            required: "Enter the server's URL.",
            validate: (value) =>
              isUri(value) ||
              "Enter a full URL, like https://mcp.example.com/mcp.",
          })}
        />
        <FieldError errors={[errors.url]} />
        <p className="text-xs text-muted-foreground">
          Everyone in this community can see this URL.
        </p>
      </Field>
      {authType === "oauth" && (
        <>
          <Field>
            <FieldLabel htmlFor="mcp-client-id">
              Client ID (optional)
            </FieldLabel>
            <Input
              id="mcp-client-id"
              autoComplete="off"
              {...register("clientId")}
            />
            <p className="text-xs text-muted-foreground">
              Only needed if the server doesn&apos;t let Habitat register
              itself. Register a client with redirect URI{" "}
              <span className="font-mono break-all">{OAUTH_CALLBACK_URL}</span>{" "}
              and enter its ID here.
            </p>
          </Field>
          <Field>
            <FieldLabel htmlFor="mcp-client-secret">
              Client secret (optional)
            </FieldLabel>
            <Input
              id="mcp-client-secret"
              type="password"
              autoComplete="off"
              {...register("clientSecret")}
            />
            <p className="text-xs text-muted-foreground">
              Stored encrypted, and never shown again.
            </p>
          </Field>
        </>
      )}
      <FieldError errors={error ? [{ message: error.message }] : []} />
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={onBack}>
          Back
        </Button>
        <Button type="submit" disabled={adding || !isValid}>
          {adding ? "Adding…" : "Add server"}
        </Button>
      </DialogFooter>
    </form>
  );
}

function RemoveServerButton({
  org,
  id,
  name,
  authManager,
}: {
  org: DidString;
  id: string;
  name: string;
  authManager: AuthManager;
}) {
  const [open, setOpen] = useState(false);

  const { mutate, isPending, error, reset } = useMutation(
    removeMcpServerMutationOptions(authManager, org),
  );

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) reset();
      }}
    >
      <DialogTrigger render={<Button variant="ghost" size="sm" />}>
        Remove
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Remove "{name}"?</DialogTitle>
        </DialogHeader>
        <p className="text-sm text-muted-foreground">
          Members' sign-ins to this server stop working. If it's added again,
          they'll need to connect again.
        </p>
        <FieldError errors={error ? [{ message: error.message }] : []} />
        <DialogFooter>
          <Button
            variant="destructive"
            disabled={isPending}
            onClick={() => mutate(id, { onSuccess: () => setOpen(false) })}
          >
            {isPending ? "Removing…" : "Remove server"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
