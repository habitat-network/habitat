import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  Separator,
} from "internal/components/ui";
import { createFileRoute } from "@tanstack/react-router";
import { ChevronDown, ShieldAlert } from "lucide-react";
import { useState } from "react";

type ConsentInfo = {
  scopes: string[];
  clientId: string;
  clientName: string;
  clientUri: string;
  logoUri: string;
  tosUri: string;
  policyUri: string;
};

export const Route = createFileRoute("/login/consent")({
  loader: async (): Promise<ConsentInfo> => {
    const res = await fetch("/oauth/consent");
    if (!res.ok) throw new Error("Failed to load consent request");
    return (await res.json()) as ConsentInfo;
  },
  component: ConsentPage,
});

// hostname pulls out just the domain of a URL for display, falling back to
// the raw string if it isn't a valid absolute URL (e.g. a bare client_id).
function hostname(uri: string): string | undefined {
  try {
    return new URL(uri).hostname;
  } catch {
    return undefined;
  }
}

// humanizeScope turns a Habitat OAuth scope string into a short human
// readable label and, when useful, a longer description — the same role the
// atproto PDS OAuth consent screen's scope descriptions play, translating
// wire-format scopes (e.g. "org:network.habitat.space?action=create") into
// something a person can decide about.
function humanizeScope(scope: string): { title: string; description: string } {
  if (scope === "atproto") {
    return {
      title: "Your atproto account",
      description: "Sign in and act as you.",
    };
  }

  const [resource, rest] = scope.split(/:(.*)/s);
  if (resource && rest !== undefined) {
    const [namespace, query] = rest.split("?");
    const actions = query
      ? Array.from(new URLSearchParams(query).getAll("action"))
      : [];

    const resourceLabel =
      resource === "org" ? "your organization's data" : resource;
    const namespaceLabel =
      !namespace || namespace === "*"
        ? undefined
        : (namespace.split(".").pop() ?? namespace);

    const actionsLabel = actions.length
      ? actions.map((a) => a[0].toUpperCase() + a.slice(1)).join(" and ")
      : "Full access to";

    return {
      title: namespaceLabel
        ? `${actionsLabel} ${namespaceLabel} records`
        : `${actionsLabel} ${resourceLabel}`,
      description: namespaceLabel
        ? `${actionsLabel} records of type "${namespace}" in ${resourceLabel}.`
        : `${actionsLabel} ${resourceLabel}.`,
    };
  }

  return { title: scope, description: "" };
}

function ConsentPage() {
  const {
    scopes,
    clientId,
    clientName,
    clientUri,
    logoUri,
    tosUri,
    policyUri,
  } = Route.useLoaderData();
  const [showDetails, setShowDetails] = useState(false);

  const displayName = clientName || hostname(clientId) || clientId;
  const idHost = hostname(clientId);
  const uriHost = clientUri ? hostname(clientUri) : undefined;
  const identityMismatch = Boolean(idHost && uriHost && idHost !== uriHost);

  return (
    <div className="flex w-full max-w-md flex-col gap-4">
      <h1 className="text-2xl font-semibold">Authorize access</h1>

      <Card>
        <CardHeader className="flex-row items-center gap-3">
          {logoUri && (
            <img
              src={logoUri}
              alt=""
              className="h-10 w-10 rounded-lg object-cover"
            />
          )}
          <div className="flex min-w-0 flex-col">
            <p className="truncate font-medium">{displayName}</p>
            {idHost && (
              <p className="truncate text-xs text-muted-foreground">{idHost}</p>
            )}
          </div>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {identityMismatch && (
            <div className="flex items-start gap-2 rounded-lg bg-destructive/10 p-3 text-xs text-destructive">
              <ShieldAlert className="mt-0.5 size-4 shrink-0" />
              <p>
                This app's homepage ({uriHost}) doesn't match the domain it
                registered from ({idHost}). Only continue if you trust this app.
              </p>
            </div>
          )}

          <p className="text-sm text-muted-foreground">
            {displayName} is asking for permission to:
          </p>

          <ul className="flex flex-col gap-2">
            {scopes.map((s: string) => {
              const { title } = humanizeScope(s);
              return (
                <li key={s} className="flex items-start gap-2 text-sm">
                  <span className="mt-1.5 size-1.5 shrink-0 rounded-full bg-foreground/50" />
                  {title}
                </li>
              );
            })}
          </ul>

          {(tosUri || policyUri) && (
            <p className="text-xs text-muted-foreground">
              By continuing, you agree to {displayName}'s{" "}
              {tosUri && (
                <a
                  href={tosUri}
                  target="_blank"
                  rel="noreferrer"
                  className="underline underline-offset-4"
                >
                  Terms of Service
                </a>
              )}
              {tosUri && policyUri && " and "}
              {policyUri && (
                <a
                  href={policyUri}
                  target="_blank"
                  rel="noreferrer"
                  className="underline underline-offset-4"
                >
                  Privacy Policy
                </a>
              )}
              .
            </p>
          )}

          <Collapsible open={showDetails} onOpenChange={setShowDetails}>
            <CollapsibleTrigger className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
              <ChevronDown
                className={`size-3 transition-transform ${showDetails ? "rotate-180" : ""}`}
              />
              Technical details
            </CollapsibleTrigger>
            <CollapsibleContent>
              <Separator className="my-2" />
              <div className="flex flex-col gap-2">
                <p className="text-xs text-muted-foreground">Client ID</p>
                <p className="text-xs break-all">{clientId}</p>
                <p className="mt-1 text-xs text-muted-foreground">
                  Requested scopes
                </p>
                <div className="flex flex-wrap gap-1">
                  {scopes.map((s: string) => (
                    <Badge key={s} variant="outline" className="font-mono">
                      {s}
                    </Badge>
                  ))}
                </div>
              </div>
            </CollapsibleContent>
          </Collapsible>
        </CardContent>
      </Card>

      <form method="POST" action="/oauth/consent">
        <fieldset className="flex flex-col gap-4">
          <Button type="submit">Authorize</Button>
        </fieldset>
      </form>
    </div>
  );
}
