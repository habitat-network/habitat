import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { AuthManager } from "internal";
import type { DidString, UriString } from "@atproto/lex";
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
  FieldError,
  FieldLabel,
  Input,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "internal/components/ui";

const AUTH_TYPE_LABEL: Record<string, string> = {
  none: "No authorization required",
  oauth: "OAuth",
};

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
            <TableHead>URL</TableHead>
            <TableHead>Auth</TableHead>
            <TableHead>Status</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {servers.map(({ server, connected }) => (
            <TableRow key={server.id}>
              <TableCell className="font-medium">{server.name}</TableCell>
              <TableCell className="text-muted-foreground font-mono text-xs">
                {server.url}
              </TableCell>
              <TableCell>
                <Badge variant="outline">
                  {AUTH_TYPE_LABEL[server.authType] ?? server.authType}
                </Badge>
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
              <TableCell colSpan={5} className="text-muted-foreground">
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

  const { mutate: disconnect, isPending: disconnecting } = useMutation({
    mutationFn: () => disconnectMcpServer(authManager, org, server.id),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["mcp", "servers", org] }),
  });

  const { mutate: authorize, isPending: authorizing } = useMutation({
    mutationFn: () =>
      startMcpAuthorization(authManager, org, server.id, window.location.href),
    onSuccess: (authorizationUrl) => {
      window.location.href = authorizationUrl;
    },
  });

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

  if (server.authType === "none") {
    return <Badge variant="secondary">No authorization needed</Badge>;
  }

  return (
    <Button
      variant="outline"
      size="sm"
      disabled={authorizing}
      onClick={() => authorize()}
    >
      {authorizing ? "Redirecting…" : "Connect"}
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
  const [url, setUrl] = useState("");
  const [description, setDescription] = useState("");
  const queryClient = useQueryClient();

  const { mutate, isPending, error } = useMutation({
    mutationFn: () =>
      addMcpServer(authManager, org, {
        name,
        url: url as UriString,
        description: description || undefined,
      }),
    async onSuccess() {
      await queryClient.invalidateQueries({
        queryKey: ["mcp", "servers", org],
      });
      setOpen(false);
      setName("");
      setUrl("");
      setDescription("");
    },
  });

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={<Button size="sm" />}>Add server</DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add an MCP server</DialogTitle>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim() && url.trim()) mutate();
          }}
        >
          <Field>
            <FieldLabel htmlFor="mcp-name">Name</FieldLabel>
            <Input
              id="mcp-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Linear"
              autoFocus
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="mcp-url">URL</FieldLabel>
            <Input
              id="mcp-url"
              type="url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="https://mcp.example.com"
            />
            <p className="text-xs text-muted-foreground">
              The gateway will probe this URL to detect whether it requires
              OAuth authorization.
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
          <FieldError errors={error ? [{ message: error.message }] : []} />
          <DialogFooter>
            <Button
              type="submit"
              disabled={isPending || !name.trim() || !url.trim()}
            >
              {isPending ? "Adding…" : "Add server"}
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
          Every member's stored credential for this server will be deleted
          too.
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
