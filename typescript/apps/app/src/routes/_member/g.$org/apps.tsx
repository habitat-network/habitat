import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { z } from "zod";
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Field,
  FieldDescription,
  FieldLabel,
  Input,
  Textarea,
  toast,
} from "internal/components/ui";
import { AppWindow, ExternalLink, Plus, Rocket, Trash2 } from "lucide-react";
import { useState } from "react";
import { CodePicker, type PickedCode } from "@/components/apps/CodePicker";
import { EmptyState, PageHeader } from "@/components/shell/PageHeader";
import { cn } from "@/lib/cn";
import { formatBytes } from "@/lib/files";
import { appsQuery } from "@/lib/queries";
import { slugify } from "@/lib/apps";
import { deleteApp, deployApp, type AppView } from "@/server/fns/apps";

// Apps: code members upload, which runs in a sandbox at its own URL. The
// list on the left, the selected app (or the new app form) on the right.

export const Route = createFileRoute("/_member/g/$org/apps")({
  validateSearch: z.object({ app: z.string().optional() }),
  loader: ({ context, params }) =>
    Promise.all([context.queryClient.ensureQueryData(appsQuery(params.org))]),
  component: AppsPage,
});

const appsKey = (org: string) => appsQuery(org).queryKey;

function AppsPage() {
  const { org } = Route.useParams();
  const { app: selectedId } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { data: apps } = useSuspenseQuery(appsQuery(org));
  const creating = selectedId === "new" || apps.length === 0;
  const selected = creating
    ? null
    : (apps.find((a) => a.uri === selectedId) ?? apps[0] ?? null);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title="Apps"
        description="Code your group deploys and runs at its own URL"
      >
        <Button onClick={() => navigate({ search: { app: "new" } })}>
          <Plus data-icon="inline-start" />
          New app
        </Button>
      </PageHeader>
      <div className="flex min-h-0 flex-1">
        <div className="flex w-80 shrink-0 flex-col gap-2 overflow-y-auto border-r p-3">
          {apps.length === 0 && (
            <EmptyState icon={<AppWindow />} title="No apps yet">
              Upload some code to deploy your group's first app.
            </EmptyState>
          )}
          {apps.map((a) => (
            <button
              key={a.uri}
              type="button"
              onClick={() => navigate({ search: { app: a.uri } })}
              className={cn(
                "flex items-center gap-3 rounded-lg border p-3 text-left",
                a.uri === selected?.uri
                  ? "border-primary bg-primary/5"
                  : "hover:bg-muted/60",
              )}
            >
              <span className="flex size-9 items-center justify-center rounded-lg bg-muted">
                <AppWindow className="size-4" />
              </span>
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{a.name}</p>
                <p className="truncate font-mono text-xs text-muted-foreground">
                  {new URL(a.url).host}
                </p>
              </div>
              <Badge variant="outline">v{a.version}</Badge>
            </button>
          ))}
        </div>
        <div className="min-w-0 flex-1 overflow-y-auto p-6">
          {creating ? (
            <NewApp
              org={org}
              onCreated={(uri) => navigate({ search: { app: uri } })}
            />
          ) : selected ? (
            <AppDetail key={selected.uri} org={org} app={selected} />
          ) : null}
        </div>
      </div>
    </div>
  );
}

function deployErrorToast(title: string) {
  return (e: Error) =>
    toast.add({ type: "error", title, description: e.message });
}

function NewApp({
  org,
  onCreated,
}: {
  org: string;
  onCreated: (uri: string) => void;
}) {
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [code, setCode] = useState<PickedCode | null>(null);
  const deploy = useMutation({
    mutationFn: () =>
      deployApp({
        data: {
          org,
          name,
          description: description || undefined,
          entrypoint: code?.entrypoint ?? "",
          modules: code?.modules ?? {},
        },
      }),
    onSuccess: async ({ uri, syncError }) => {
      if (syncError) toast.add({ type: "info", title: syncError });
      await queryClient.invalidateQueries({ queryKey: appsKey(org) });
      onCreated(uri);
    },
    onError: deployErrorToast("Couldn't deploy the app"),
  });
  const ready =
    name.trim() !== "" &&
    code !== null &&
    code.entrypoint !== "" &&
    Object.keys(code.modules).length > 0;
  return (
    <form
      className="mx-auto flex max-w-xl flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        if (ready) deploy.mutate();
      }}
    >
      <h2 className="text-xl font-semibold">New app</h2>
      <Field>
        <FieldLabel htmlFor="app-name">Name</FieldLabel>
        <Input
          id="app-name"
          value={name}
          maxLength={200}
          onChange={(e) => setName(e.target.value)}
        />
        <FieldDescription>
          {name.trim()
            ? `Its address will start with ${slugify(name)}-`
            : "Shown to your group. Its address is made from it."}
        </FieldDescription>
      </Field>
      <Field>
        <FieldLabel htmlFor="app-description">Description</FieldLabel>
        <Textarea
          id="app-description"
          value={description}
          maxLength={2000}
          onChange={(e) => setDescription(e.target.value)}
        />
      </Field>
      <Field>
        <FieldLabel>Code</FieldLabel>
        <CodePicker value={code} onChange={setCode} />
        <FieldDescription>
          A Worker: the entrypoint's default export has a{" "}
          <code>fetch(request, env)</code> handler. Bundle dependencies first.
          Apps can't make network requests yet.
        </FieldDescription>
      </Field>
      <div>
        <Button type="submit" disabled={!ready || deploy.isPending}>
          <Rocket data-icon="inline-start" />
          {deploy.isPending ? "Deploying…" : "Deploy"}
        </Button>
      </div>
    </form>
  );
}

function AppDetail({ org, app }: { org: string; app: AppView }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate({ from: Route.fullPath });
  const [code, setCode] = useState<PickedCode | null>(null);
  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: appsKey(org) });
  const redeploy = useMutation({
    mutationFn: () =>
      deployApp({
        data: {
          org,
          uri: app.uri,
          name: app.name,
          description: app.description ?? undefined,
          entrypoint: code?.entrypoint ?? "",
          modules: code?.modules ?? {},
        },
      }),
    onSuccess: async ({ syncError }) => {
      toast.add({
        type: syncError ? "info" : "success",
        title: syncError ?? `Deployed version ${app.version + 1}`,
      });
      setCode(null);
      await invalidate();
    },
    onError: deployErrorToast("Couldn't deploy"),
  });
  const remove = useMutation({
    mutationFn: () => deleteApp({ data: { org, uri: app.uri } }),
    onSuccess: async () => {
      await invalidate();
      await navigate({ search: {} });
    },
    onError: deployErrorToast("Couldn't delete the app"),
  });
  const ready =
    code !== null &&
    code.entrypoint !== "" &&
    Object.keys(code.modules).length > 0;
  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-6">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <h2 className="text-xl font-semibold">{app.name}</h2>
          {app.description && (
            <p className="text-sm text-muted-foreground">{app.description}</p>
          )}
        </div>
        <Button render={<a href={app.url} target="_blank" rel="noreferrer" />}>
          <ExternalLink data-icon="inline-start" />
          Open
        </Button>
        <Button
          variant="destructive"
          disabled={remove.isPending}
          onClick={() => {
            if (window.confirm(`Delete ${app.name}? Its URL stops working.`)) {
              remove.mutate();
            }
          }}
        >
          <Trash2 data-icon="inline-start" />
          Delete
        </Button>
      </div>
      <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
        <dt className="text-muted-foreground">URL</dt>
        <dd className="break-all font-mono">
          <a
            className="underline"
            href={app.url}
            target="_blank"
            rel="noreferrer"
          >
            {app.url}
          </a>
        </dd>
        <dt className="text-muted-foreground">Version</dt>
        <dd>{app.version}</dd>
        <dt className="text-muted-foreground">Entrypoint</dt>
        <dd className="font-mono">{app.entrypoint}</dd>
        <dt className="text-muted-foreground">Code</dt>
        <dd>{formatBytes(app.codeSize)}</dd>
        <dt className="text-muted-foreground">Deployed</dt>
        <dd>
          {new Date(app.updatedAt).toLocaleString()}
          {app.updatedByName && ` by ${app.updatedByName}`}
        </dd>
      </dl>
      <Card>
        <CardHeader>
          <CardTitle>Deploy a new version</CardTitle>
          <CardDescription>
            Upload the app's code again. The new version replaces the current
            one at the same URL.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <CodePicker value={code} onChange={setCode} />
          <div>
            <Button
              disabled={!ready || redeploy.isPending}
              onClick={() => redeploy.mutate()}
            >
              <Rocket data-icon="inline-start" />
              {redeploy.isPending
                ? "Deploying…"
                : `Deploy version ${app.version + 1}`}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
