import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import Nango from "@nangohq/frontend";
import type { AuthManager } from "internal";
import type { DidString } from "@atproto/lex";
import {
  addMcpServer,
  disconnectMcpServer,
  removeMcpServer,
  startMcpAuthorization,
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

// isUri narrows s to the lexicon's uri string format.
function isUri(s: string): s is `${string}:${string}` {
  return URL.canParse(s);
}

export function McpServersEditor({
  org,
  servers,
  isAdmin,
  authManager,
}: {
  org: DidString;
  servers: McpServerWithStatus[];
  isAdmin: boolean;
  authManager: AuthManager;
}) {
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h2 className="text-base font-semibold">
          MCP servers ({servers.length})
        </h2>
        {isAdmin && <AddServerDialog org={org} authManager={authManager} />}
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
                {isAdmin && (
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
  const [authorizing, setAuthorizing] = useState(false);

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["mcp", "servers", org] });

  const { mutate: disconnect, isPending: disconnecting } = useMutation({
    mutationFn: () => disconnectMcpServer(authManager, org, server.id),
    onSuccess: invalidate,
  });

  const authorize = async () => {
    setAuthorizing(true);
    try {
      const sessionToken = await startMcpAuthorization(
        authManager,
        org,
        server.id,
      );
      const nango = new Nango();
      const connect = nango.openConnectUI({
        sessionToken,
        onEvent: async (event) => {
          if (event.type === "connect") {
            await invalidate();
          }
          if (event.type === "connect" || event.type === "close") {
            setAuthorizing(false);
          }
          if (event.type === "error") {
            setAuthorizing(false);
            toast.add({ type: "error", title: "Failed to connect" });
          }
        },
      });
      connect.open();
    } catch {
      setAuthorizing(false);
      toast.add({ type: "error", title: "Failed to start authorization" });
    }
  };

  if (server.authType === "manual") {
    return <Badge variant="secondary">Shared</Badge>;
  }

  if (connected) {
    return (
      <div className="flex items-center gap-2">
        <Badge variant="secondary">Connected</Badge>
        <Button
          variant="ghost"
          size="sm"
          disabled={disconnecting}
          onClick={() => disconnect()}
        >
          Disconnect
        </Button>
      </div>
    );
  }

  return (
    <Button
      variant="outline"
      size="sm"
      disabled={authorizing}
      onClick={() => void authorize()}
    >
      {authorizing ? "Connecting…" : "Connect"}
    </Button>
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
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [authType, setAuthType] = useState<AuthType>("oauth");
  const [url, setUrl] = useState("");
  const [adding, setAdding] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const queryClient = useQueryClient();

  const reset = () => {
    setName("");
    setDescription("");
    setAuthType("oauth");
    setUrl("");
    setAdding(false);
    setError(null);
  };

  const canSubmit = SERVER_NAME_PATTERN.test(name) && isUri(url);

  // Adding a server just writes its record; nobody signs in here. For an
  // oauth server, members (including this admin) connect afterward with the
  // "Connect" button, which drives Nango's Connect UI.
  const submit = async () => {
    if (!isUri(url)) return;
    setAdding(true);
    setError(null);
    try {
      await addMcpServer(authManager, org, {
        name,
        description: description || undefined,
        url,
        authType,
      });
      await queryClient.invalidateQueries({
        queryKey: ["mcp", "servers", org],
      });
      setOpen(false);
      reset();
    } catch (e) {
      setAdding(false);
      setError(e instanceof Error ? e.message : "Failed to add server");
    }
  };

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
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (!canSubmit) return;
            void submit();
          }}
        >
          <Field>
            <FieldLabel htmlFor="mcp-name">Name</FieldLabel>
            <Input
              id="mcp-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="cloudflare"
              autoFocus
            />
            <p className="text-xs text-muted-foreground">
              Letters, numbers, hyphens, and underscores only. Unique within
              this community, and can't be changed later.
            </p>
          </Field>
          <Field>
            <FieldLabel htmlFor="mcp-description">
              Description (optional)
            </FieldLabel>
            <Input
              id="mcp-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel>How members connect</FieldLabel>
            <RadioGroup
              value={authType}
              onValueChange={(value) =>
                setAuthType(value === "manual" ? "manual" : "oauth")
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
          </Field>
          <Field>
            <FieldLabel htmlFor="mcp-url">Server URL</FieldLabel>
            <Input
              id="mcp-url"
              type="url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="https://mcp.example.com/mcp"
            />
            <p className="text-xs text-muted-foreground">
              Everyone in this community can see this URL.
            </p>
          </Field>
          <FieldError errors={error ? [{ message: error }] : []} />
          <DialogFooter>
            <Button type="submit" disabled={adding || !canSubmit}>
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
  const queryClient = useQueryClient();

  const { mutate, isPending, error } = useMutation({
    mutationFn: () => removeMcpServer(authManager, org, id),
    async onSuccess() {
      await queryClient.invalidateQueries({
        queryKey: ["mcp", "servers", org],
      });
      setOpen(false);
    },
  });

  return (
    <Dialog open={open} onOpenChange={setOpen}>
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
            onClick={() => mutate()}
          >
            {isPending ? "Removing…" : "Remove server"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
