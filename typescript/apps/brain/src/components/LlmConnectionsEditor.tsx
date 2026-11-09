import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
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
} from "internal/components/ui";
import { CpuIcon } from "lucide-react";
import {
  addLlmConnection,
  getOrgConfigAccess,
  listOrgLlmConnections,
  removeLlmConnection,
  updateLlmConnection,
} from "@/server/functions";
import type { LlmConnection } from "@/server/habitat";
import { configAccessKey } from "./McpServersEditor";

// Mirrors llmgateway.namePattern (Go). The name is the connection's record
// key and the model ID API clients request.
const NAME_PATTERN = /^[A-Za-z0-9._-]{1,64}$/;

// The provider APIs pear can call; see llmgateway.Provider (Go).
type Provider = "openai" | "anthropic" | "google" | "ollama";

const PROVIDERS: {
  value: Provider;
  title: string;
  description: string;
  modelPlaceholder: string;
  needsApiKey: boolean;
  // Whether the provider's base URL can be set. Required for ollama.
  baseUrl: "none" | "optional" | "required";
}[] = [
  {
    value: "openai",
    title: "OpenAI",
    description: "Or any OpenAI-compatible API, with a base URL.",
    modelPlaceholder: "gpt-4o",
    needsApiKey: true,
    baseUrl: "optional",
  },
  {
    value: "anthropic",
    title: "Anthropic",
    description: "Claude models.",
    modelPlaceholder: "claude-sonnet-4-5",
    needsApiKey: true,
    baseUrl: "optional",
  },
  {
    value: "google",
    title: "Google",
    description: "Gemini models.",
    modelPlaceholder: "gemini-2.5-flash",
    needsApiKey: true,
    baseUrl: "none",
  },
  {
    value: "ollama",
    title: "Ollama",
    description: "Self-hosted, no API key.",
    modelPlaceholder: "llama3.1",
    needsApiKey: false,
    baseUrl: "required",
  },
];

const providerInfo = (provider: string) =>
  PROVIDERS.find((p) => p.value === provider);

// isUri narrows s to the lexicon's uri string format.
function isUri(s: string): s is `${string}:${string}` {
  return URL.canParse(s);
}

function isHttpUrl(s: string) {
  return URL.canParse(s) && /^https?:$/.test(new URL(s).protocol);
}

export const llmConnectionsKey = (org: string) =>
  ["llm-connections", org] as const;

export function LlmConnectionsEditor({ org }: { org: string }) {
  const queryClient = useQueryClient();
  const { data: connections, isPending } = useQuery({
    queryKey: llmConnectionsKey(org),
    queryFn: () => listOrgLlmConnections({ data: { org } }),
  });
  const { data: access } = useQuery({
    queryKey: configAccessKey(org),
    queryFn: () => getOrgConfigAccess({ data: { org } }),
  });
  const canConfigure = access?.models ?? false;
  // The chat's model picker reads the gateway's models, so refresh it too.
  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: llmConnectionsKey(org) });
    await queryClient.invalidateQueries({ queryKey: ["models", org] });
  };

  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-base font-semibold">Models</h2>
          <p className="text-sm text-muted-foreground">
            The language models members can chat with.
          </p>
        </div>
        {canConfigure && <ConnectionDialog org={org} onSaved={refresh} />}
      </div>
      {isPending ? (
        <Spinner />
      ) : connections && connections.length > 0 ? (
        <ul className="flex flex-col divide-y rounded-xl border">
          {connections.map((c) => (
            <li key={c.id} className="flex items-center gap-3 p-4">
              <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                <CpuIcon className="size-4" />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate font-medium">{c.name}</span>
                  <Badge variant="outline">
                    {providerInfo(c.provider)?.title ?? c.provider}
                  </Badge>
                </div>
                <p className="truncate text-sm text-muted-foreground">
                  <span className="font-mono">{c.model}</span>
                  {c.description ? ` · ${c.description}` : ""}
                </p>
              </div>
              <Badge variant="secondary">
                {c.hasApiKey ? "Key stored" : "No key"}
              </Badge>
              {canConfigure && (
                <div className="flex shrink-0 gap-1">
                  <ConnectionDialog
                    org={org}
                    connection={c}
                    onSaved={refresh}
                  />
                  <RemoveConnectionButton
                    org={org}
                    id={c.id}
                    name={c.name}
                    onRemoved={refresh}
                  />
                </div>
              )}
            </li>
          ))}
        </ul>
      ) : (
        <p className="rounded-xl border border-dashed p-6 text-sm text-muted-foreground">
          No models configured yet.
          {canConfigure ? " Add one so members can start chatting." : ""}
        </p>
      )}
    </section>
  );
}

// ConnectionDialog adds a connection, or edits connection when given. An
// edited connection's name and provider can't change, and its API key is only
// replaced when a new one is entered.
function ConnectionDialog({
  org,
  connection,
  onSaved,
}: {
  org: string;
  connection?: LlmConnection;
  onSaved: () => Promise<void>;
}) {
  const [open, setOpen] = useState(false);
  const editing = connection !== undefined;
  const initial = {
    name: connection?.name ?? "",
    description: connection?.description ?? "",
    provider: (connection?.provider as Provider | undefined) ?? "openai",
    model: connection?.model ?? "",
    baseUrl: connection?.baseUrl ?? "",
  };
  const [name, setName] = useState(initial.name);
  const [description, setDescription] = useState(initial.description);
  const [providerValue, setProviderValue] = useState<Provider>(
    initial.provider,
  );
  const [model, setModel] = useState(initial.model);
  const [baseUrl, setBaseUrl] = useState(initial.baseUrl);
  const [apiKey, setApiKey] = useState("");
  const provider = providerInfo(providerValue) ?? PROVIDERS[0];

  const save = useMutation({
    mutationFn: async () => {
      const url = baseUrl.trim();
      const baseUrlField = url && isUri(url) ? url : undefined;
      if (editing) {
        await updateLlmConnection({
          data: {
            org,
            id: connection.id,
            description,
            model,
            baseUrl: baseUrlField,
            apiKey: apiKey || undefined,
          },
        });
      } else {
        await addLlmConnection({
          data: {
            org,
            name,
            description: description || undefined,
            provider: providerValue,
            model,
            baseUrl: baseUrlField,
            apiKey: apiKey || undefined,
          },
        });
      }
      await onSaved();
    },
    onSuccess: () => {
      setOpen(false);
      reset();
    },
  });

  const reset = () => {
    setName(initial.name);
    setDescription(initial.description);
    setProviderValue(initial.provider);
    setModel(initial.model);
    setBaseUrl(initial.baseUrl);
    setApiKey("");
    save.reset();
  };

  const baseUrlOk =
    provider.baseUrl === "none" ||
    (baseUrl ? isHttpUrl(baseUrl) : provider.baseUrl === "optional");
  const keyOk =
    !provider.needsApiKey || apiKey !== "" || (editing && connection.hasApiKey);
  const valid =
    (editing || NAME_PATTERN.test(name)) && model !== "" && baseUrlOk && keyOk;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) reset();
      }}
    >
      <DialogTrigger
        render={
          editing ? <Button variant="ghost" size="sm" /> : <Button size="sm" />
        }
      >
        {editing ? "Edit" : "Add model"}
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {editing ? `Edit "${connection.name}"` : "Add a model"}
          </DialogTitle>
        </DialogHeader>
        <form className="flex flex-col gap-4" onSubmit={submit}>
          {!editing && (
            <>
              <Field>
                <FieldLabel htmlFor="llm-name">Name</FieldLabel>
                <Input
                  id="llm-name"
                  placeholder="gpt-4o"
                  autoFocus
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                />
                <p
                  className={`text-xs ${name && !NAME_PATTERN.test(name) ? "text-destructive" : "text-muted-foreground"}`}
                >
                  The model ID API clients request. 1-64 letters, numbers, dots,
                  hyphens or underscores; unique within this community and can't
                  be changed later.
                </p>
              </Field>
              <Field>
                <FieldLabel>Provider</FieldLabel>
                <div className="grid grid-cols-2 gap-2">
                  {PROVIDERS.map((p) => (
                    <button
                      key={p.value}
                      type="button"
                      aria-pressed={providerValue === p.value}
                      onClick={() => setProviderValue(p.value)}
                      className={`rounded-lg border p-3 text-left text-sm transition-colors ${
                        providerValue === p.value
                          ? "border-primary bg-primary/5"
                          : "hover:bg-muted/50"
                      }`}
                    >
                      <span className="block font-medium">{p.title}</span>
                      <span className="text-xs text-muted-foreground">
                        {p.description}
                      </span>
                    </button>
                  ))}
                </div>
              </Field>
            </>
          )}
          <Field>
            <FieldLabel htmlFor="llm-description">
              Description (optional)
            </FieldLabel>
            <Input
              id="llm-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="llm-model">Provider model ID</FieldLabel>
            <Input
              id="llm-model"
              placeholder={provider.modelPlaceholder}
              value={model}
              onChange={(e) => setModel(e.target.value)}
            />
          </Field>
          {provider.baseUrl !== "none" && (
            <Field>
              <FieldLabel htmlFor="llm-base-url">
                Base URL{provider.baseUrl === "optional" ? " (optional)" : ""}
              </FieldLabel>
              <Input
                id="llm-base-url"
                type="url"
                placeholder={
                  provider.value === "ollama"
                    ? "http://localhost:11434"
                    : "https://openrouter.ai/api/v1"
                }
                value={baseUrl}
                onChange={(e) => setBaseUrl(e.target.value)}
              />
              {baseUrl && !baseUrlOk && (
                <p className="text-xs text-destructive">
                  Enter a full http or https URL.
                </p>
              )}
            </Field>
          )}
          {provider.needsApiKey && (
            <Field>
              <FieldLabel htmlFor="llm-api-key">API key</FieldLabel>
              <Input
                id="llm-api-key"
                type="password"
                autoComplete="off"
                placeholder={
                  editing && connection.hasApiKey
                    ? "Leave blank to keep the stored key"
                    : ""
                }
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                Stored encrypted and never shown again.
              </p>
            </Field>
          )}
          <FieldError
            errors={save.error ? [{ message: save.error.message }] : []}
          />
          <DialogFooter>
            <Button type="submit" disabled={save.isPending || !valid}>
              {save.isPending
                ? editing
                  ? "Saving…"
                  : "Adding…"
                : editing
                  ? "Save"
                  : "Add model"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function RemoveConnectionButton({
  org,
  id,
  name,
  onRemoved,
}: {
  org: string;
  id: string;
  name: string;
  onRemoved: () => Promise<void>;
}) {
  const [open, setOpen] = useState(false);
  const { mutate, isPending, error, reset } = useMutation({
    mutationFn: () => removeLlmConnection({ data: { org, id } }),
    onSuccess: async () => {
      await onRemoved();
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
          Its stored API key will be deleted too. Clients requesting this model
          will get an error.
        </p>
        <FieldError errors={error ? [{ message: error.message }] : []} />
        <DialogFooter>
          <Button
            variant="destructive"
            disabled={isPending}
            onClick={() => mutate()}
          >
            {isPending ? "Removing…" : "Remove model"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
