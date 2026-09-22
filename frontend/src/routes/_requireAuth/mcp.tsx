import {
  addMcpServer,
  connectMcpServer,
  disconnectMcpServer,
  listMcpServersQueryOptions,
  removeMcpServer,
} from "@/queries/mcp";
import { getAdminsQueryOptions } from "@/queries/org";
import { Button, Input } from "internal";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
  toast,
} from "internal/components/ui";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { useState } from "react";
import type { UriString } from "@atproto/lex";
import type { network } from "api";

type AuthType = "none" | "api_key";
type McpServer = network.habitat.mcp.defs.Server;

const AUTH_TYPE_LABEL: Record<AuthType, string> = {
  none: "No credential required",
  api_key: "API key / bearer token",
};

export const Route = createFileRoute("/_requireAuth/mcp")({
  async loader({ context }) {
    const { authManager, queryClient } = context;
    const [serversData, adminsData] = await Promise.all([
      queryClient.fetchQuery(listMcpServersQueryOptions(authManager)),
      queryClient.fetchQuery(getAdminsQueryOptions(authManager)),
    ]);
    const authInfo = authManager.getAuthInfo();
    const callerDid = authInfo?.did ?? "";
    const isAdmin = adminsData.admins.some((a) => a.did === callerDid);
    return { servers: serversData.servers, isAdmin };
  },
  component: McpServersPage,
});

function McpServersPage() {
  const { servers, isAdmin } = Route.useLoaderData();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const router = useRouter();

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ["mcp", "servers"] });
    router.invalidate();
  };

  return (
    <div className="flex flex-col gap-8">
      <div>
        <h1 className="text-xl font-semibold mb-1">MCP servers</h1>
        <p className="text-sm text-muted-foreground">
          MCP servers configured for your org. Connect your own credential to
          a server to start using it.
        </p>
      </div>

      <div className="flex flex-col gap-4">
        {servers.length === 0 && (
          <p className="text-sm text-muted-foreground">
            No MCP servers have been configured for this org yet.
          </p>
        )}
        {servers.map(({ server, connected }) => (
          <McpServerRow
            key={server.id}
            server={server}
            connected={connected}
            isAdmin={isAdmin}
            authManager={authManager}
            onChange={invalidate}
          />
        ))}
      </div>

      {isAdmin && (
        <AddServerForm authManager={authManager} onAdded={invalidate} />
      )}
    </div>
  );
}

function McpServerRow({
  server,
  connected,
  isAdmin,
  authManager,
  onChange,
}: {
  server: McpServer;
  connected: boolean;
  isAdmin: boolean;
  authManager: Parameters<typeof connectMcpServer>[0];
  onChange: () => Promise<void>;
}) {
  const [credential, setCredential] = useState("");

  const { mutate: connect, isPending: connecting } = useMutation({
    mutationFn: () => connectMcpServer(authManager, server.id, credential),
    onSuccess: async () => {
      setCredential("");
      toast.add({ title: `Connected to ${server.name}` });
      await onChange();
    },
    onError: () => toast.add({ type: "error", title: "Failed to connect" }),
  });

  const { mutate: disconnect, isPending: disconnecting } = useMutation({
    mutationFn: () => disconnectMcpServer(authManager, server.id),
    onSuccess: async () => {
      toast.add({ title: `Disconnected from ${server.name}` });
      await onChange();
    },
  });

  const { mutate: remove, isPending: removing } = useMutation({
    mutationFn: () => removeMcpServer(authManager, server.id),
    onSuccess: onChange,
  });

  return (
    <div className="border rounded-md p-4 flex flex-col gap-3">
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="font-medium">{server.name}</div>
          <div className="text-xs font-mono text-muted-foreground">
            {server.url}
          </div>
          {server.description && (
            <p className="text-sm text-muted-foreground mt-1">
              {server.description}
            </p>
          )}
        </div>
        {isAdmin && (
          <Button
            variant="destructive"
            size="xs"
            disabled={removing}
            onClick={() => remove()}
          >
            Remove
          </Button>
        )}
      </div>

      <div className="flex items-center gap-2">
        {connected ? (
          <>
            <span className="text-sm text-green-600">Connected</span>
            <Button
              variant="outline"
              size="xs"
              disabled={disconnecting}
              onClick={() => disconnect()}
            >
              Disconnect
            </Button>
          </>
        ) : server.authType === "none" ? (
          <Button variant="outline" size="xs" onClick={() => connect()}>
            Connect
          </Button>
        ) : (
          <form
            className="flex gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              connect();
            }}
          >
            <Input
              className="font-mono text-xs"
              type="password"
              placeholder="API key / token"
              value={credential}
              onChange={(e) => setCredential(e.target.value)}
            />
            <Button
              type="submit"
              variant="outline"
              size="xs"
              disabled={!credential || connecting}
            >
              Connect
            </Button>
          </form>
        )}
      </div>
    </div>
  );
}

function AddServerForm({
  authManager,
  onAdded,
}: {
  authManager: Parameters<typeof addMcpServer>[0];
  onAdded: () => Promise<void>;
}) {
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [description, setDescription] = useState("");
  const [authType, setAuthType] = useState<AuthType>("api_key");

  const { mutate: add, isPending } = useMutation({
    mutationFn: () =>
      addMcpServer(authManager, {
        name,
        url: url as UriString,
        description,
        authType,
      }),
    onSuccess: async () => {
      setName("");
      setUrl("");
      setDescription("");
      setAuthType("api_key");
      toast.add({ title: "MCP server added" });
      await onAdded();
    },
    onError: () => toast.add({ type: "error", title: "Failed to add server" }),
  });

  return (
    <section className="border rounded-md p-4">
      <h2 className="text-lg font-semibold mb-3">Add an MCP server</h2>
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          add();
        }}
      >
        <label className="flex flex-col gap-1 text-sm">
          Name
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          URL
          <Input
            type="url"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://mcp.example.com"
            required
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          Description (optional)
          <Input
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          Authentication
          <Select
            value={authType}
            onValueChange={(v) => setAuthType(v as AuthType)}
          >
            <SelectTrigger>
              <SelectValue>{(v) => AUTH_TYPE_LABEL[v as AuthType]}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value="api_key">
                  {AUTH_TYPE_LABEL.api_key}
                </SelectItem>
                <SelectItem value="none">{AUTH_TYPE_LABEL.none}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        </label>
        <div>
          <Button type="submit" disabled={!name || !url || isPending}>
            Add server
          </Button>
        </div>
      </form>
    </section>
  );
}
