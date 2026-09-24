import { useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import Nango from "@nangohq/frontend";
import type { AuthManager } from "internal";
import type { DidString } from "@atproto/lex";
import {
  addManualMcpServer,
  beginAddMcpServer,
  cancelAddMcpServer,
  completeAddMcpServer,
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

type HeaderRow = { name: string; value: string };

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
  const [headers, setHeaders] = useState<HeaderRow[]>([]);
  const [connecting, setConnecting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Tracks the in-progress add's server ID between opening Nango's Connect UI
  // and it reporting success or being abandoned, so a "close" without a
  // preceding "connect" knows to cancel it.
  const pendingIdRef = useRef<string | null>(null);
  const queryClient = useQueryClient();

  const reset = () => {
    setName("");
    setDescription("");
    setAuthType("oauth");
    setUrl("");
    setHeaders([]);
    setConnecting(false);
    setError(null);
    pendingIdRef.current = null;
  };

  const submitManual = async () => {
    if (!isUri(url)) return;
    setConnecting(true);
    setError(null);
    try {
      await addManualMcpServer(authManager, org, {
        name,
        description: description || undefined,
        url,
        headers: headers
          .filter((h) => h.name !== "")
          .map((h) => ({ name: h.name, value: h.value })),
      });
      await queryClient.invalidateQueries({
        queryKey: ["mcp", "servers", org],
      });
      setOpen(false);
      reset();
    } catch (e) {
      setConnecting(false);
      setError(e instanceof Error ? e.message : "Failed to add server");
    }
  };

  const updateHeader = (index: number, patch: Partial<HeaderRow>) =>
    setHeaders((rows) =>
      rows.map((row, i) => (i === index ? { ...row, ...patch } : row)),
    );

  const canSubmit =
    SERVER_NAME_PATTERN.test(name) && (authType === "oauth" || isUri(url));

  const submit = async () => {
    setConnecting(true);
    setError(null);
    try {
      const { id, sessionToken } = await beginAddMcpServer(authManager, org, {
        name,
        description: description || undefined,
      });
      pendingIdRef.current = id;

      const nango = new Nango();
      const connect = nango.openConnectUI({
        sessionToken,
        onEvent: async (event) => {
          if (event.type === "connect") {
            pendingIdRef.current = null;
            try {
              await completeAddMcpServer(authManager, org, {
                id,
                name,
                description: description || undefined,
              });
              await queryClient.invalidateQueries({
                queryKey: ["mcp", "servers", org],
              });
              setOpen(false);
              reset();
            } catch {
              setConnecting(false);
              toast.add({ type: "error", title: "Failed to add server" });
            }
          }
          if (event.type === "close") {
            setConnecting(false);
            if (pendingIdRef.current) {
              await cancelAddMcpServer(authManager, org, pendingIdRef.current);
              pendingIdRef.current = null;
            }
          }
          if (event.type === "error") {
            setConnecting(false);
            if (pendingIdRef.current) {
              await cancelAddMcpServer(authManager, org, pendingIdRef.current);
              pendingIdRef.current = null;
            }
            toast.add({ type: "error", title: "Failed to connect" });
          }
        },
      });
      connect.open();
    } catch (e) {
      setConnecting(false);
      setError(e instanceof Error ? e.message : "Failed to start");
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
            void (authType === "manual" ? submitManual() : submit());
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
                    <FieldTitle>API key or no auth</FieldTitle>
                    <FieldDescription>
                      You enter the URL and any headers once, and everyone in
                      this community connects with them.
                    </FieldDescription>
                  </FieldContent>
                  <RadioGroupItem value="manual" id="mcp-auth-manual" />
                </Field>
              </FieldLabel>
            </RadioGroup>
          </Field>
          {authType === "manual" ? (
            <>
              <Field>
                <FieldLabel htmlFor="mcp-url">Server URL</FieldLabel>
                <Input
                  id="mcp-url"
                  type="url"
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  placeholder="https://mcp.example.com/mcp"
                />
              </Field>
              <Field>
                <FieldLabel>Headers (optional)</FieldLabel>
                {headers.map((header, i) => (
                  <div key={i} className="flex gap-2">
                    <Input
                      aria-label="Header name"
                      value={header.name}
                      onChange={(e) =>
                        updateHeader(i, { name: e.target.value })
                      }
                      placeholder="Authorization"
                    />
                    <Input
                      aria-label="Header value"
                      type="password"
                      value={header.value}
                      onChange={(e) =>
                        updateHeader(i, { value: e.target.value })
                      }
                      placeholder="Bearer …"
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() =>
                        setHeaders((rows) => rows.filter((_, j) => j !== i))
                      }
                    >
                      Remove
                    </Button>
                  </div>
                ))}
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="self-start"
                  onClick={() =>
                    setHeaders((rows) => [...rows, { name: "", value: "" }])
                  }
                >
                  Add header
                </Button>
                <p className="text-xs text-muted-foreground">
                  The URL and header values are stored encrypted and can't be
                  viewed after saving.
                </p>
              </Field>
            </>
          ) : (
            <p className="text-xs text-muted-foreground">
              You'll enter the server's URL and connect to it in the next step.
            </p>
          )}
          <FieldError errors={error ? [{ message: error }] : []} />
          <DialogFooter>
            <Button type="submit" disabled={connecting || !canSubmit}>
              {connecting
                ? authType === "manual"
                  ? "Adding…"
                  : "Connecting…"
                : authType === "manual"
                  ? "Add server"
                  : "Continue"}
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
