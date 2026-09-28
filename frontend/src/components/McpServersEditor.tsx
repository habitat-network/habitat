import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Controller, useForm } from "react-hook-form";
import Nango from "@nangohq/frontend";
import type { AuthManager } from "internal";
import type { DidString } from "@atproto/lex";
import {
  addMcpServerMutationOptions,
  disconnectMcpServerMutationOptions,
  mcpServersQueryKey,
  removeMcpServerMutationOptions,
  startMcpAuthorizationMutationOptions,
  type McpServerWithStatus,
} from "@/queries/mcp";
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
// server's record key and, once connected, the namespace its tools are
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
}

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
                {canConfigureMcp && (
                  <RemoveServerButton
                    org={org}
                    id={server.id}
                    name={server.name}
                    authManager={authManager}
                  />
                )}
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
  // Whether Nango's Connect UI is open. That lifecycle belongs to Nango's SDK
  // rather than to a mutation, so it's tracked separately.
  const [connectUiOpen, setConnectUiOpen] = useState(false);

  const {
    mutate: disconnect,
    isPending: disconnecting,
    error: disconnectError,
  } = useMutation(disconnectMcpServerMutationOptions(authManager, org));

  const {
    mutate: startAuthorization,
    isPending: starting,
    error: startError,
  } = useMutation(startMcpAuthorizationMutationOptions(authManager, org));

  const authorize = () =>
    startAuthorization(server.id, {
      onSuccess(sessionToken) {
        setConnectUiOpen(true);
        const nango = new Nango();
        const connect = nango.openConnectUI({
          sessionToken,
          onEvent: async (event) => {
            if (event.type === "connect") {
              await queryClient.invalidateQueries({
                queryKey: mcpServersQueryKey(org),
              });
            }
            if (event.type === "connect" || event.type === "close") {
              setConnectUiOpen(false);
            }
            if (event.type === "error") {
              setConnectUiOpen(false);
              toast.add({ type: "error", title: "Failed to connect" });
            }
          },
        });
        connect.open();
      },
    });

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

  const connecting = starting || connectUiOpen;
  return (
    <div className="flex flex-col gap-1">
      <Button
        variant="outline"
        size="sm"
        className="self-start"
        disabled={connecting}
        onClick={authorize}
      >
        {connecting ? "Connecting…" : "Connect"}
      </Button>
      <FieldError
        errors={startError ? [{ message: startError.message }] : []}
      />
    </div>
  );
}

function AddServerDialog({
  org,
  authManager,
}: {
  org: DidString;
  authManager: AuthManager;
}) {
  const [open, setOpen] = useState(false);

  const {
    register,
    handleSubmit,
    control,
    reset: resetForm,
    formState: { errors, isValid },
  } = useForm<AddServerFormValues>({
    mode: "onChange",
    defaultValues: { name: "", description: "", authType: "oauth", url: "" },
  });

  const {
    mutate: add,
    isPending: adding,
    error,
    reset: resetMutation,
  } = useMutation(addMcpServerMutationOptions(authManager, org));

  const reset = () => {
    resetForm();
    resetMutation();
  };

  // Adding a server just writes its record; nobody signs in here. For an
  // oauth server, members (including this admin) connect afterward with the
  // "Connect" button, which drives Nango's Connect UI.
  const submit = handleSubmit(({ name, description, authType, url }) => {
    if (!isUri(url)) return;
    add(
      { name, description: description || undefined, url, authType },
      {
        onSuccess() {
          setOpen(false);
          reset();
        },
      },
    );
  });

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) reset();
      }}
    >
      <DialogTrigger render={<Button size="sm" />}>Add server</DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add an MCP server</DialogTitle>
        </DialogHeader>
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
                  message:
                    "Use 1-64 letters, numbers, hyphens, or underscores.",
                },
              })}
            />
            <FieldError errors={[errors.name]} />
            <p className="text-xs text-muted-foreground">
              Letters, numbers, hyphens, and underscores only. Unique within
              this community, and can't be changed later.
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
                          Each member signs in with their own account. Use this
                          for servers that support MCP sign-in.
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
                          Every member connects to it automatically. Use this
                          for servers that don't require sign-in.
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
          <FieldError errors={error ? [{ message: error.message }] : []} />
          <DialogFooter>
            <Button type="submit" disabled={adding || !isValid}>
              {adding ? "Adding…" : "Add server"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
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
          Every member's stored credential for this server will be deleted too.
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
