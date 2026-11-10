import { useState, type ReactNode } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import type { DidString } from "@atproto/lex";
import type { AuthManager } from "internal";
import {
  Badge,
  Button,
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
  Input,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Spinner,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Textarea,
} from "internal/components/ui";
import {
  mcpDebugMutationOptions,
  mcpDebugQueryOptions,
  type McpDebugOutput,
  type McpServerWithStatus,
} from "@/queries/mcp";

// What a failed debug request looks like to the user: the server's raw error,
// plus where it failed (see network.habitat.mcp.debug's errorKind).
interface DebugOutcome {
  pending: boolean;
  output?: McpDebugOutput;
  // The request never reached the server: pear itself failed.
  requestError?: string;
}

// parse reads the debug result's JSON text, or undefined if there is none or
// it isn't JSON.
function parse(output?: McpDebugOutput): Record<string, unknown> | undefined {
  if (!output?.ok || !output.result) return undefined;
  try {
    return JSON.parse(output.result) as Record<string, unknown>;
  } catch {
    return undefined;
  }
}

function itemsOf(output: McpDebugOutput | undefined, key: string) {
  const items = parse(output)?.[key];
  return Array.isArray(items)
    ? (items as Record<string, unknown>[])
    : undefined;
}

export function McpDebugger({
  org,
  servers,
  authManager,
  initialServer,
}: {
  org: DidString;
  servers: McpServerWithStatus[];
  authManager: AuthManager;
  initialServer?: string;
}) {
  const [serverId, setServerId] = useState<string | undefined>(
    initialServer ?? servers.find((s) => s.connected)?.server.id,
  );
  const selected = servers.find((s) => s.server.id === serverId);

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <h2 className="text-base font-semibold">MCP debugger</h2>
        <p className="text-sm text-muted-foreground">
          Make raw MCP requests to a server as yourself and see exactly what it
          answers, errors included.
        </p>
      </div>
      <Field className="max-w-md">
        <FieldLabel>Server</FieldLabel>
        <Select
          value={serverId ?? null}
          onValueChange={(value) => setServerId(value ?? undefined)}
        >
          <SelectTrigger>
            <SelectValue placeholder="Choose an MCP server">
              {selected?.server.name}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {servers.map(({ server, connected }) => (
              <SelectItem key={server.id} value={server.id}>
                {server.name}
                {!connected && " (not connected)"}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {selected?.server.url && (
          <FieldDescription className="font-mono">
            {selected.server.url}
          </FieldDescription>
        )}
      </Field>
      {selected ? (
        // Keyed so switching servers starts every request and form over.
        <ServerDebugger
          key={selected.server.id}
          org={org}
          id={selected.server.id}
          authManager={authManager}
        />
      ) : (
        <p className="text-sm text-muted-foreground">
          {servers.length === 0
            ? "No MCP servers configured yet."
            : "Choose a server to probe."}
        </p>
      )}
    </div>
  );
}

function ServerDebugger({
  org,
  id,
  authManager,
}: {
  org: DidString;
  id: string;
  authManager: AuthManager;
}) {
  const tools = useQuery(
    mcpDebugQueryOptions(authManager, org, id, "tools/list"),
  );
  const resources = useQuery(
    mcpDebugQueryOptions(authManager, org, id, "resources/list"),
  );
  const prompts = useQuery(
    mcpDebugQueryOptions(authManager, org, id, "prompts/list"),
  );
  const [toolName, setToolName] = useState("");
  const [toolArgs, setToolArgs] = useState("{}");
  const [resourceUri, setResourceUri] = useState("");
  const [tab, setTab] = useState("tools");

  const outcome = (q: typeof tools): DebugOutcome => ({
    pending: q.isFetching,
    output: q.data,
    requestError: q.error?.message,
  });

  return (
    <Tabs value={tab} onValueChange={setTab}>
      <div className="flex items-center justify-between gap-2">
        <TabsList>
          <TabsTrigger value="tools">
            Tools{" "}
            <TabStatus
              outcome={outcome(tools)}
              count={itemsOf(tools.data, "tools")?.length}
            />
          </TabsTrigger>
          <TabsTrigger value="resources">
            Resources{" "}
            <TabStatus
              outcome={outcome(resources)}
              count={itemsOf(resources.data, "resources")?.length}
            />
          </TabsTrigger>
          <TabsTrigger value="prompts">
            Prompts{" "}
            <TabStatus
              outcome={outcome(prompts)}
              count={itemsOf(prompts.data, "prompts")?.length}
            />
          </TabsTrigger>
        </TabsList>
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            void tools.refetch();
            void resources.refetch();
            void prompts.refetch();
          }}
        >
          Re-run all
        </Button>
      </div>

      <TabsContent value="tools" className="flex flex-col gap-6">
        <ListResult
          title="tools/list"
          outcome={outcome(tools)}
          items={itemsOf(tools.data, "tools")}
          primary="name"
          onUse={(item) => {
            setToolName(String(item.name));
            setToolArgs("{}");
          }}
          onRetry={() => void tools.refetch()}
        />
        <InvokeTool
          org={org}
          id={id}
          authManager={authManager}
          name={toolName}
          setName={setToolName}
          args={toolArgs}
          setArgs={setToolArgs}
        />
      </TabsContent>

      <TabsContent value="resources" className="flex flex-col gap-6">
        <ListResult
          title="resources/list"
          outcome={outcome(resources)}
          items={itemsOf(resources.data, "resources")}
          primary="uri"
          onUse={(item) => setResourceUri(String(item.uri))}
          onRetry={() => void resources.refetch()}
        />
        <ReadResource
          org={org}
          id={id}
          authManager={authManager}
          uri={resourceUri}
          setUri={setResourceUri}
        />
      </TabsContent>

      <TabsContent value="prompts" className="flex flex-col gap-6">
        <ListResult
          title="prompts/list"
          outcome={outcome(prompts)}
          items={itemsOf(prompts.data, "prompts")}
          primary="name"
          onRetry={() => void prompts.refetch()}
        />
      </TabsContent>
    </Tabs>
  );
}

// TabStatus summarizes a list request in its tab: a spinner while it runs, an
// error mark if it failed, else how many items it returned.
function TabStatus({
  outcome,
  count,
}: {
  outcome: DebugOutcome;
  count?: number;
}) {
  if (outcome.pending) return <Spinner className="size-3" />;
  if (outcome.requestError || outcome.output?.ok === false) {
    return <Badge variant="destructive">error</Badge>;
  }
  return <Badge variant="secondary">{count ?? 0}</Badge>;
}

// Section frames one request's request name, timing and result.
function Section({
  title,
  outcome,
  actions,
  children,
}: {
  title: string;
  outcome: DebugOutcome;
  actions?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-3 rounded-xl border p-4">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <h3 className="font-mono text-sm font-semibold">{title}</h3>
          {outcome.pending ? (
            <Spinner className="size-4" />
          ) : outcome.output ? (
            <>
              <Badge variant={outcome.output.ok ? "secondary" : "destructive"}>
                {outcome.output.ok ? "ok" : "error"}
              </Badge>
              <span className="text-xs text-muted-foreground">
                {outcome.output.durationMs} ms
              </span>
            </>
          ) : null}
        </div>
        {actions}
      </div>
      {children}
    </div>
  );
}

// OutcomeBody shows an outcome's raw error, or its raw result when
// showResult.
function OutcomeBody({
  outcome,
  showResult = false,
}: {
  outcome: DebugOutcome;
  showResult?: boolean;
}) {
  const { output, requestError } = outcome;
  if (requestError) {
    return (
      <Raw tone="error" label="Request to pear failed" text={requestError} />
    );
  }
  if (output && !output.ok) {
    return (
      <Raw
        tone="error"
        label={output.errorKind ? `Error (${output.errorKind})` : "Error"}
        text={output.error ?? "unknown error"}
      />
    );
  }
  if (showResult && output?.result) {
    return <Raw label="Raw result" text={output.result} />;
  }
  return null;
}

// Raw prints text verbatim, in a box that scrolls rather than wraps, so
// pasted-in-from-a-server errors keep their shape.
function Raw({
  label,
  text,
  tone,
}: {
  label: string;
  text: string;
  tone?: "error";
}) {
  return (
    <div className="flex flex-col gap-1">
      <span
        className={`text-xs font-medium ${tone === "error" ? "text-destructive" : "text-muted-foreground"}`}
      >
        {label}
      </span>
      <pre
        className={`max-h-96 overflow-auto rounded-lg border p-3 font-mono text-xs whitespace-pre-wrap break-words ${tone === "error" ? "border-destructive/40 bg-destructive/5" : "bg-muted/40"}`}
      >
        {text}
      </pre>
    </div>
  );
}

function ListResult({
  title,
  outcome,
  items,
  primary,
  onUse,
  onRetry,
}: {
  title: string;
  outcome: DebugOutcome;
  items?: Record<string, unknown>[];
  primary: string;
  onUse?: (item: Record<string, unknown>) => void;
  onRetry: () => void;
}) {
  return (
    <Section
      title={title}
      outcome={outcome}
      actions={
        <Button variant="ghost" size="sm" onClick={onRetry}>
          Retry
        </Button>
      }
    >
      <OutcomeBody outcome={outcome} />
      {items && items.length === 0 && (
        <p className="text-sm text-muted-foreground">
          The server returned an empty list.
        </p>
      )}
      {items && items.length > 0 && (
        <ul className="flex flex-col divide-y rounded-lg border">
          {items.map((item, i) => (
            <li
              key={`${String(item[primary])}-${i}`}
              className="flex items-start justify-between gap-3 p-3"
            >
              <div className="min-w-0">
                <div className="truncate font-mono text-sm font-medium">
                  {String(item[primary])}
                </div>
                {typeof item.description === "string" && item.description && (
                  <div className="line-clamp-2 text-sm text-muted-foreground">
                    {item.description}
                  </div>
                )}
              </div>
              {onUse && (
                <Button variant="outline" size="sm" onClick={() => onUse(item)}>
                  Use
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      {outcome.output?.ok && outcome.output.result && (
        <details>
          <summary className="cursor-pointer text-xs text-muted-foreground">
            Raw result
          </summary>
          <div className="mt-2">
            <Raw label="Raw result" text={outcome.output.result} />
          </div>
        </details>
      )}
    </Section>
  );
}

function InvokeTool({
  org,
  id,
  authManager,
  name,
  setName,
  args,
  setArgs,
}: {
  org: DidString;
  id: string;
  authManager: AuthManager;
  name: string;
  setName: (name: string) => void;
  args: string;
  setArgs: (args: string) => void;
}) {
  const call = useMutation(mcpDebugMutationOptions(authManager, org));
  const argsError = jsonError(args);
  return (
    <Section
      title="tools/call"
      outcome={{
        pending: call.isPending,
        output: call.data,
        requestError: call.error?.message,
      }}
    >
      <Field>
        <FieldLabel>Tool name</FieldLabel>
        <Input
          className="font-mono"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="e.g. search_issues"
        />
      </Field>
      <Field data-invalid={argsError ? true : undefined}>
        <FieldLabel>Arguments (JSON)</FieldLabel>
        <Textarea
          className="min-h-28 font-mono text-xs"
          value={args}
          onChange={(e) => setArgs(e.target.value)}
          spellCheck={false}
          aria-invalid={argsError ? true : undefined}
        />
        <FieldError errors={argsError ? [{ message: argsError }] : []} />
      </Field>
      <Button
        className="self-start"
        disabled={!name || !!argsError || call.isPending}
        onClick={() =>
          call.mutate({
            id,
            method: "tools/call",
            name,
            arguments: args.trim() || undefined,
          })
        }
      >
        {call.isPending ? "Calling…" : "Call tool"}
      </Button>
      <OutcomeBody
        outcome={{
          pending: call.isPending,
          output: call.data,
          requestError: call.error?.message,
        }}
        showResult
      />
    </Section>
  );
}

function ReadResource({
  org,
  id,
  authManager,
  uri,
  setUri,
}: {
  org: DidString;
  id: string;
  authManager: AuthManager;
  uri: string;
  setUri: (uri: string) => void;
}) {
  const read = useMutation(mcpDebugMutationOptions(authManager, org));
  const outcome = {
    pending: read.isPending,
    output: read.data,
    requestError: read.error?.message,
  };
  return (
    <Section title="resources/read" outcome={outcome}>
      <Field>
        <FieldLabel>Resource URI</FieldLabel>
        <Input
          className="font-mono"
          value={uri}
          onChange={(e) => setUri(e.target.value)}
          placeholder="e.g. repo://owner/name/contents"
        />
      </Field>
      <Button
        className="self-start"
        disabled={!uri || read.isPending}
        onClick={() => read.mutate({ id, method: "resources/read", uri })}
      >
        {read.isPending ? "Reading…" : "Read resource"}
      </Button>
      <OutcomeBody outcome={outcome} showResult />
    </Section>
  );
}

// jsonError says why text isn't a JSON object, or undefined if it is (or is
// blank, which sends no arguments).
function jsonError(text: string): string | undefined {
  if (!text.trim()) return undefined;
  try {
    const value: unknown = JSON.parse(text);
    if (typeof value !== "object" || value === null || Array.isArray(value)) {
      return "Arguments must be a JSON object";
    }
  } catch (e) {
    return e instanceof Error ? e.message : "Invalid JSON";
  }
  return undefined;
}
