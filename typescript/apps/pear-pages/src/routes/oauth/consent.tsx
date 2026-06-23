import { Button } from "internal/components/ui";
import { createFileRoute } from "@tanstack/react-router";

// Org admin OAuth consent page (migrated from internal/oauthserver/consent.html).
// pear redirects here once an org-DID OAuth login requires explicit admin
// approval. Client metadata and requested scopes are fetched from
// /oauth/consent/data (scoped to the same consent session cookie as this
// page's form post), and the decision is posted back to /oauth/consent,
// which pear redirects accordingly (an authorization code, or an error, sent
// to the client's redirect URI).
type ConsentData = {
  clientName: string;
  clientUri: string;
  logoUri: string;
  scopes: string[];
  orgHandle: string;
};

export const Route = createFileRoute("/oauth/consent")({
  loader: async (): Promise<ConsentData> => {
    const res = await fetch("/oauth/consent/data");
    if (!res.ok) throw new Error("Failed to load consent request");
    return res.json();
  },
  component: ConsentPage,
});

function ConsentPage() {
  const { clientName, clientUri, logoUri, scopes, orgHandle } =
    Route.useLoaderData();

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
        <h1 className="text-lg font-semibold">{clientName || clientUri}</h1>
      </div>
      <p className="mb-6 text-sm text-muted-foreground break-all">
        wants to access your organization ({orgHandle})
      </p>
      {scopes.length > 0 && (
        <div className="mb-6 rounded-lg bg-muted p-3 text-sm">
          <p className="mb-2 font-medium">This will allow access to:</p>
          <ul className="list-disc pl-5">
            {scopes.map((scope) => (
              <li key={scope}>{scope}</li>
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
