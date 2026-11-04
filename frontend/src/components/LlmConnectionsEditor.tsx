import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Controller, useForm } from "react-hook-form";
import type { AuthManager } from "internal";
import type { DidString } from "@atproto/lex";
import {
  addLlmConnectionMutationOptions,
  removeLlmConnectionMutationOptions,
  updateLlmConnectionMutationOptions,
  type LlmConnection,
} from "@/queries/llm";
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
    description:
      "OpenAI, or any OpenAI-compatible API such as OpenRouter, by setting a base URL.",
    modelPlaceholder: "gpt-4o",
    needsApiKey: true,
    baseUrl: "optional",
  },
  {
    value: "anthropic",
    title: "Anthropic",
    description: "Claude models through Anthropic's API.",
    modelPlaceholder: "claude-sonnet-4-5",
    needsApiKey: true,
    baseUrl: "optional",
  },
  {
    value: "google",
    title: "Google",
    description: "Gemini models through the Gemini API.",
    modelPlaceholder: "gemini-2.5-flash",
    needsApiKey: true,
    baseUrl: "none",
  },
  {
    value: "ollama",
    title: "Ollama",
    description: "A self-hosted Ollama server. Needs no API key.",
    modelPlaceholder: "llama3.1",
    needsApiKey: false,
    baseUrl: "required",
  },
];

function providerInfo(provider: string) {
  return PROVIDERS.find((p) => p.value === provider);
}

// isUri narrows s to the lexicon's uri string format.
function isUri(s: string): s is `${string}:${string}` {
  return URL.canParse(s);
}

function isHttpUrl(s: string) {
  return URL.canParse(s) && /^https?:$/.test(new URL(s).protocol);
}

interface ConnectionFormValues {
  name: string;
  description: string;
  provider: Provider;
  model: string;
  baseUrl: string;
  apiKey: string;
}

// orgLlmBaseUrl is the base URL of the org's OpenAI-compatible API
// (internal/pearserver llm_openai.go), served from the API origin. It's
// where OpenAI clients point, with an access token as the API key.
function orgLlmBaseUrl(org: DidString) {
  return `https://${import.meta.env.VITE_HABITAT_DOMAIN}/llm/${org}/v1`;
}

export function LlmConnectionsEditor({
  org,
  connections,
  canConfigure,
  authManager,
}: {
  org: DidString;
  connections: LlmConnection[];
  canConfigure: boolean;
  authManager: AuthManager;
}) {
  return (
    <div className="flex flex-col gap-6">
      <OrgLlmBaseUrl org={org} />
      <div className="flex flex-col gap-4">
        <div className="flex items-center justify-between">
          <h2 className="text-base font-semibold">
            Models ({connections.length})
          </h2>
          {canConfigure && (
            <ConnectionDialog org={org} authManager={authManager} />
          )}
        </div>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Provider</TableHead>
              <TableHead>Model</TableHead>
              <TableHead>API key</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {connections.map((connection) => (
              <TableRow key={connection.id}>
                <TableCell className="font-medium">
                  {connection.name}
                  {connection.description && (
                    <div className="text-xs font-normal text-muted-foreground">
                      {connection.description}
                    </div>
                  )}
                </TableCell>
                <TableCell>
                  {providerInfo(connection.provider)?.title ??
                    connection.provider}
                </TableCell>
                <TableCell className="font-mono text-sm">
                  {connection.model}
                </TableCell>
                <TableCell>
                  <Badge variant="secondary">
                    {connection.hasApiKey ? "Stored" : "None"}
                  </Badge>
                </TableCell>
                <TableCell className="text-right">
                  {canConfigure && (
                    <div className="flex justify-end gap-1">
                      <ConnectionDialog
                        org={org}
                        authManager={authManager}
                        connection={connection}
                      />
                      <RemoveConnectionButton
                        org={org}
                        id={connection.id}
                        name={connection.name}
                        authManager={authManager}
                      />
                    </div>
                  )}
                </TableCell>
              </TableRow>
            ))}
            {connections.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="text-muted-foreground">
                  No models configured yet.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}

// OrgLlmBaseUrl shows the org's OpenAI-compatible base URL with a copy
// button, for adding to OpenAI clients.
function OrgLlmBaseUrl({ org }: { org: DidString }) {
  const url = orgLlmBaseUrl(org);
  return (
    <div className="flex flex-col gap-2">
      <h2 className="text-base font-semibold">OpenAI-compatible API</h2>
      <p className="text-sm text-muted-foreground">
        Use this base URL in any OpenAI client, with a Habitat access token as
        the API key. Each model below is requested by its name.
      </p>
      <div className="flex gap-2">
        <Input readOnly value={url} className="font-mono" />
        <Button
          variant="outline"
          onClick={async () => {
            try {
              await navigator.clipboard.writeText(url);
              toast.add({ type: "success", title: "Copied base URL" });
            } catch {
              toast.add({ type: "error", title: "Failed to copy base URL" });
            }
          }}
        >
          Copy
        </Button>
      </div>
    </div>
  );
}

// ConnectionDialog adds a connection, or edits connection when given. An
// edited connection's name and provider can't change, and its API key is
// only replaced when a new one is entered.
function ConnectionDialog({
  org,
  authManager,
  connection,
}: {
  org: DidString;
  authManager: AuthManager;
  connection?: LlmConnection;
}) {
  const [open, setOpen] = useState(false);
  const editing = connection !== undefined;
  const defaultValues: ConnectionFormValues = {
    name: connection?.name ?? "",
    description: connection?.description ?? "",
    provider: (connection?.provider as Provider | undefined) ?? "openai",
    model: connection?.model ?? "",
    baseUrl: connection?.baseUrl ?? "",
    apiKey: "",
  };

  const {
    register,
    handleSubmit,
    control,
    watch,
    reset: resetForm,
    formState: { errors, isValid },
  } = useForm<ConnectionFormValues>({ mode: "onChange", defaultValues });
  const provider = providerInfo(watch("provider")) ?? PROVIDERS[0];

  const add = useMutation(addLlmConnectionMutationOptions(authManager, org));
  const update = useMutation(
    updateLlmConnectionMutationOptions(authManager, org),
  );
  const { isPending, error } = editing ? update : add;

  const reset = () => {
    resetForm(defaultValues);
    add.reset();
    update.reset();
  };

  const submit = handleSubmit((values) => {
    const baseUrl = values.baseUrl.trim();
    if (baseUrl && !isUri(baseUrl)) return;
    const baseUrlField = baseUrl && isUri(baseUrl) ? baseUrl : undefined;
    const onSuccess = () => {
      setOpen(false);
      reset();
    };
    if (editing) {
      update.mutate(
        {
          id: connection.id,
          description: values.description,
          model: values.model,
          baseUrl: baseUrlField,
          apiKey: values.apiKey || undefined,
        },
        { onSuccess },
      );
    } else {
      add.mutate(
        {
          name: values.name,
          description: values.description || undefined,
          provider: values.provider,
          model: values.model,
          baseUrl: baseUrlField,
          apiKey: values.apiKey || undefined,
        },
        { onSuccess },
      );
    }
  });

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
                  {...register("name", {
                    required: "Enter a name.",
                    pattern: {
                      value: NAME_PATTERN,
                      message:
                        "Use 1-64 letters, numbers, dots, hyphens, or underscores.",
                    },
                  })}
                />
                <FieldError errors={[errors.name]} />
                <p className="text-xs text-muted-foreground">
                  The model ID API clients request. Unique within this
                  community, and can't be changed later.
                </p>
              </Field>
              <Field>
                <FieldLabel>Provider</FieldLabel>
                <Controller
                  control={control}
                  name="provider"
                  render={({ field: { value, onChange } }) => (
                    <RadioGroup
                      value={value}
                      onValueChange={(next) => {
                        const found = providerInfo(next);
                        if (found) onChange(found.value);
                      }}
                    >
                      {PROVIDERS.map((p) => (
                        <FieldLabel key={p.value} htmlFor={`llm-${p.value}`}>
                          <Field orientation="horizontal">
                            <FieldContent>
                              <FieldTitle>{p.title}</FieldTitle>
                              <FieldDescription>
                                {p.description}
                              </FieldDescription>
                            </FieldContent>
                            <RadioGroupItem
                              value={p.value}
                              id={`llm-${p.value}`}
                            />
                          </Field>
                        </FieldLabel>
                      ))}
                    </RadioGroup>
                  )}
                />
              </Field>
            </>
          )}
          <Field>
            <FieldLabel htmlFor="llm-description">
              Description (optional)
            </FieldLabel>
            <Input id="llm-description" {...register("description")} />
          </Field>
          <Field>
            <FieldLabel htmlFor="llm-model">Provider model ID</FieldLabel>
            <Input
              id="llm-model"
              placeholder={provider.modelPlaceholder}
              {...register("model", { required: "Enter the model ID." })}
            />
            <FieldError errors={[errors.model]} />
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
                {...register("baseUrl", {
                  required:
                    provider.baseUrl === "required"
                      ? "Enter the server's URL."
                      : false,
                  validate: (value) =>
                    !value ||
                    isHttpUrl(value) ||
                    "Enter a full http or https URL.",
                })}
              />
              <FieldError errors={[errors.baseUrl]} />
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
                {...register("apiKey", {
                  required:
                    editing && connection.hasApiKey
                      ? false
                      : "Enter an API key.",
                })}
              />
              <FieldError errors={[errors.apiKey]} />
              <p className="text-xs text-muted-foreground">
                Stored encrypted and never shown again.
              </p>
            </Field>
          )}
          <FieldError errors={error ? [{ message: error.message }] : []} />
          <DialogFooter>
            <Button type="submit" disabled={isPending || !isValid}>
              {isPending
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
  authManager,
}: {
  org: DidString;
  id: string;
  name: string;
  authManager: AuthManager;
}) {
  const [open, setOpen] = useState(false);

  const { mutate, isPending, error, reset } = useMutation(
    removeLlmConnectionMutationOptions(authManager, org),
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
          Its stored API key will be deleted too. Clients requesting this model
          will get an error.
        </p>
        <FieldError errors={error ? [{ message: error.message }] : []} />
        <DialogFooter>
          <Button
            variant="destructive"
            disabled={isPending}
            onClick={() => mutate(id, { onSuccess: () => setOpen(false) })}
          >
            {isPending ? "Removing…" : "Remove model"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
