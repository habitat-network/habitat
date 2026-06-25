import { Button } from "internal/components/ui";
import { createFileRoute } from "@tanstack/react-router";

// Org admin OAuth consent page. pear redirects here once an org-DID OAuth
// login requires explicit admin approval, passing everything this page renders
// as query params: the requesting client's name/uri/logo, the requested
// scopes, and the org handle. (pear fetches the client's public metadata
// server-side so this page doesn't have to make a cross-origin request.) The
// decision is posted back to /oauth/consent, which pear redirects accordingly
// (an authorization code, or an error, sent to the client's redirect URI).
export const Route = createFileRoute("/oauth/consent")({
  validateSearch: (search: Record<string, unknown>) => ({
    clientId: String(search.clientId ?? ""),
    clientName: String(search.clientName ?? ""),
    clientUri: String(search.clientUri ?? ""),
    logoUri: String(search.logoUri ?? ""),
    scope: String(search.scope ?? ""),
    orgHandle: String(search.orgHandle ?? ""),
  }),
  component: ConsentPage,
});

function ConsentPage() {
  const { clientId, clientName, clientUri, logoUri, scope, orgHandle } =
    Route.useSearch();
  const scopes = scope.split(/\s+/).filter(Boolean);

  return (
    <main className="w-96 rounded-[0.625rem] border border-border bg-card p-8 shadow-sm">
      <div className="mb-4 flex items-center gap-3">
        {logoUri && (
          <img
            src={logoUri}
            alt=""
            className="size-10 rounded-lg object-contain"
          />
        )}
        <h1 className="text-lg font-semibold break-all">
          {clientName || clientUri || clientId || "An application"}
        </h1>
      </div>
      <p className="mb-6 text-sm text-muted-foreground break-all">
        wants to access your organization ({orgHandle})
      </p>
      {scopes.length > 0 && (
        <div className="mb-6 rounded-lg bg-muted p-3 text-sm">
          <p className="mb-2 font-medium">This will allow access to:</p>
          <ul className="list-disc pl-5">
            {scopes.map((s) => (
              <li key={s}>{s}</li>
            ))}
          </ul>
        </div>
      )}
      <form method="POST" action="/oauth/consent" className="flex gap-2.5">
        <Button
          type="submit"
          name="decision"
          value="deny"
          variant="secondary"
          className="flex-1"
        >
          Deny
        </Button>
        <Button type="submit" name="decision" value="allow" className="flex-1">
          Allow
        </Button>
      </form>
    </main>
  );
}
