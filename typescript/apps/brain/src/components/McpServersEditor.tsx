import { useState, type FormEvent } from "react";
import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
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
  Spinner,
  toast,
} from "internal/components/ui";
import {
  McpTokenDialog,
  useMcpConnect,
  type McpServerWithStatus,
} from "internal/mcp";
import { PlugIcon } from "lucide-react";
import {
  CONNECTORS,
  ConnectorIcon,
  connectorFor,
  type Connector,
} from "@/lib/connectors";
import {
  addMcpServer,
  disconnectMcpServer,
  getOrgConfigAccess,
  listOrgMcpServers,
  removeMcpServer,
  startMcpConnect,
} from "@/server/functions";

// Mirrors mcpgateway.serverNamePattern (Go). The name doubles as the server's
// ID and the namespace its tools are exposed under, so it is a plain slug.
const SERVER_NAME_PATTERN = /^[A-Za-z0-9_-]{1,64}$/;

export const mcpServersKey = (org: string) => ["mcp-servers", org] as const;
export const configAccessKey = (org: string) => ["config-access", org] as const;

const refresh = (queryClient: QueryClient, org: string) =>
  queryClient.invalidateQueries({ queryKey: mcpServersKey(org) });

// isUri narrows s to the lexicon's uri string format.
function isUri(s: string): s is `${string}:${string}` {
  return URL.canParse(s);
}

export function McpServersEditor({ org }: { org: string }) {
  const { data: servers, isPending } = useQuery({
    queryKey: mcpServersKey(org),
    queryFn: () => listOrgMcpServers({ data: { org } }),
  });
  const { data: access } = useQuery({
    queryKey: configAccessKey(org),
    queryFn: () => getOrgConfigAccess({ data: { org } }),
  });
  const canConfigure = access?.mcp ?? false;

  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-base font-semibold">MCP servers</h2>
          <p className="text-sm text-muted-foreground">
            Tools Brain can use. Each member connects their own account.
          </p>
        </div>
        {canConfigure && <AddServerDialog org={org} />}
      </div>
      {isPending ? (
        <Spinner />
      ) : servers && servers.length > 0 ? (
        <ul className="flex flex-col divide-y rounded-xl border">
          {servers.map((s) => (
            <ServerRow
              key={s.server.id}
              org={org}
              status={s}
              canConfigure={canConfigure}
            />
          ))}
        </ul>
      ) : (
        <p className="rounded-xl border border-dashed p-6 text-sm text-muted-foreground">
          No MCP servers configured yet.
          {canConfigure ? " Add one to give Brain more tools." : ""}
        </p>
      )}
    </section>
  );
}

function ServerRow({
  org,
  status: { server, connected },
  canConfigure,
}: {
  org: string;
  status: McpServerWithStatus;
  canConfigure: boolean;
}) {
  const queryClient = useQueryClient();
  const connector = connectorFor(server);
  const { connect, connecting, error } = useMcpConnect({
    start: (id) => startMcpConnect({ data: { org, id } }),
    onSettled: () => void refresh(queryClient, org),
  });
  const disconnect = useMutation({
    mutationFn: (id: string) => disconnectMcpServer({ data: { org, id } }),
    onSuccess: () => refresh(queryClient, org),
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't disconnect",
        description: e.message,
      }),
  });

  return (
    <li className="flex items-center gap-3 p-4">
      <ConnectorIcon
        connector={connector}
        fallback={<PlugIcon className="size-4" />}
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate font-medium">{server.name}</span>
          {server.authType === "manual" ? (
            <Badge variant="secondary">Shared</Badge>
          ) : connected ? (
            <Badge variant="secondary">Connected</Badge>
          ) : null}
        </div>
        <p className="truncate text-sm text-muted-foreground">
          {server.description || connector?.description}
        </p>
        {error && <p className="text-xs text-destructive">{error.message}</p>}
      </div>
      <div className="flex shrink-0 items-center gap-1">
        {server.authType === "manual" ? null : connected ? (
          <Button
            variant="ghost"
            size="sm"
            disabled={disconnect.isPending}
            onClick={() => disconnect.mutate(server.id)}
          >
            {disconnect.isPending ? "Disconnecting…" : "Disconnect"}
          </Button>
        ) : server.authType === "token" ? (
          <McpTokenDialog
            serverName={server.name}
            save={async (token) => {
              await startMcpConnect({ data: { org, id: server.id, token } });
              await refresh(queryClient, org);
            }}
          />
        ) : (
          <Button
            variant="outline"
            size="sm"
            disabled={connecting}
            onClick={() => connect(server.id)}
          >
            {connecting ? "Connecting…" : "Connect"}
          </Button>
        )}
        {canConfigure && (
          <RemoveServerButton org={org} id={server.id} name={server.name} />
        )}
      </div>
    </li>
  );
}

// AddServerDialog first asks which connector to add, then shows the form for
// it: a connector-specific one for a known connector, or the generic one for a
// custom server.
function AddServerDialog({ org }: { org: string }) {
  const [open, setOpen] = useState(false);
  const [choice, setChoice] = useState<Connector | "custom" | null>(null);

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
        ) : (
          <AddServerForm
            org={org}
            connector={choice === "custom" ? undefined : choice}
            onBack={() => setChoice(null)}
            onDone={() => {
              setOpen(false);
              setChoice(null);
            }}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function ConnectorPicker({
  onSelect,
}: {
  onSelect: (choice: Connector | "custom") => void;
}) {
  const options: { key: string; choice: Connector | "custom" }[] = [
    ...CONNECTORS.map((c) => ({ key: c.id, choice: c })),
    { key: "custom", choice: "custom" },
  ];
  return (
    <div className="flex flex-col gap-2">
      {options.map(({ key, choice }) => {
        const connector = choice === "custom" ? undefined : choice;
        return (
          <button
            key={key}
            type="button"
            onClick={() => onSelect(choice)}
            className="flex items-center gap-3 rounded-lg border p-3 text-left transition-colors hover:bg-muted/50 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
          >
            <ConnectorIcon
              connector={connector}
              fallback={<PlugIcon className="size-4" />}
            />
            <span className="flex flex-col">
              <span className="font-medium">
                {connector ? connector.name : "Custom server"}
              </span>
              <span className="text-xs text-muted-foreground">
                {connector
                  ? connector.description
                  : "Connect any MCP server by URL."}
              </span>
            </span>
          </button>
        );
      })}
    </div>
  );
}

// AddServerForm adds a known connector, which asks only for a name (pear fills
// in the rest from its preset), or, with no connector, a custom server by URL.
// Adding registers pear with the server's authorization server, if it has one,
// but nobody signs in here: members, including this admin, connect afterward.
function AddServerForm({
  org,
  connector,
  onBack,
  onDone,
}: {
  org: string;
  connector: Connector | undefined;
  onBack: () => void;
  onDone: () => void;
}) {
  const queryClient = useQueryClient();
  const [name, setName] = useState(connector?.defaultName ?? "");
  const [description, setDescription] = useState("");
  const [url, setUrl] = useState("");
  const [authType, setAuthType] = useState<"oauth" | "manual">("oauth");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");

  const nameOk = SERVER_NAME_PATTERN.test(name);
  const valid = nameOk && (connector !== undefined || isUri(url));

  const add = useMutation({
    mutationFn: addMcpServer,
    onSuccess: async () => {
      await refresh(queryClient, org);
      onDone();
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (connector) {
      add.mutate({
        data: {
          org,
          name,
          description: connector.description,
          preset: connector.preset,
        },
      });
    } else if (isUri(url)) {
      const oauth = authType === "oauth";
      add.mutate({
        data: {
          org,
          name,
          description: description || undefined,
          url,
          authType,
          clientId: (oauth && clientId) || undefined,
          clientSecret: (oauth && clientId && clientSecret) || undefined,
        },
      });
    }
  };

  return (
    <form className="flex flex-col gap-4" onSubmit={submit}>
      {connector && (
        <div className="flex items-start gap-3">
          <ConnectorIcon connector={connector} fallback={null} />
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
        </div>
      )}
      <Field>
        <FieldLabel htmlFor="mcp-name">Name</FieldLabel>
        <Input
          id="mcp-name"
          placeholder="cloudflare"
          value={name}
          autoFocus
          onChange={(e) => setName(e.target.value)}
        />
        <p
          className={`text-xs ${name && !nameOk ? "text-destructive" : "text-muted-foreground"}`}
        >
          1-64 letters, numbers, hyphens or underscores. Unique within this
          community, and can't be changed later.
        </p>
      </Field>
      {!connector && (
        <>
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
            <FieldLabel htmlFor="mcp-url">Server URL</FieldLabel>
            <Input
              id="mcp-url"
              type="url"
              placeholder="https://mcp.example.com/mcp"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              Everyone in this community can see this URL.
            </p>
          </Field>
          <Field>
            <FieldLabel>How members connect</FieldLabel>
            <div className="grid grid-cols-2 gap-2">
              {(
                [
                  ["oauth", "Sign in with OAuth", "Each member signs in."],
                  ["manual", "No auth", "Everyone is connected."],
                ] as const
              ).map(([value, title, hint]) => (
                <button
                  key={value}
                  type="button"
                  aria-pressed={authType === value}
                  onClick={() => setAuthType(value)}
                  className={`rounded-lg border p-3 text-left text-sm transition-colors ${
                    authType === value
                      ? "border-primary bg-primary/5"
                      : "hover:bg-muted/50"
                  }`}
                >
                  <span className="block font-medium">{title}</span>
                  <span className="text-xs text-muted-foreground">{hint}</span>
                </button>
              ))}
            </div>
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
                  value={clientId}
                  onChange={(e) => setClientId(e.target.value)}
                />
                <p className="text-xs text-muted-foreground">
                  Only needed if the server doesn&apos;t let Habitat register
                  itself.
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
                  value={clientSecret}
                  onChange={(e) => setClientSecret(e.target.value)}
                />
              </Field>
            </>
          )}
        </>
      )}
      <FieldError errors={add.error ? [{ message: add.error.message }] : []} />
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={onBack}>
          Back
        </Button>
        <Button type="submit" disabled={add.isPending || !valid}>
          {add.isPending
            ? "Adding…"
            : connector
              ? `Add ${connector.name}`
              : "Add server"}
        </Button>
      </DialogFooter>
    </form>
  );
}

function RemoveServerButton({
  org,
  id,
  name,
}: {
  org: string;
  id: string;
  name: string;
}) {
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const { mutate, isPending, error, reset } = useMutation({
    mutationFn: () => removeMcpServer({ data: { org, id } }),
    onSuccess: async () => {
      await refresh(queryClient, org);
      setOpen(false);
    },
  });

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
            onClick={() => mutate()}
          >
            {isPending ? "Removing…" : "Remove server"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
