import { Button } from "internal/components/ui";
import { createFileRoute } from "@tanstack/react-router";

type ConsentInfo = {
  scopes: string[];
  clientName: string;
  clientUri: string;
  logoUri: string;
};

export const Route = createFileRoute("/login/consent")({
  loader: async (): Promise<ConsentInfo> => {
    const res = await fetch("/oauth/consent");
    if (!res.ok) throw new Error("Failed to load consent request");
    return (await res.json()) as ConsentInfo;
  },
  component: ConsentPage,
});

function ConsentPage() {
  const { scopes, clientName, clientUri, logoUri } = Route.useLoaderData();

  return (
    <div className="flex w-full max-w-md flex-col gap-4">
      <h1 className="text-2xl font-semibold">Authorize</h1>
      {clientName && (
        <div className="flex items-center gap-3">
          {logoUri && <img src={logoUri} alt="" className="h-8 w-8 rounded" />}
          <div>
            <p className="font-medium">{clientName}</p>
            {clientUri && (
              <p className="text-xs text-muted-foreground">{clientUri}</p>
            )}
          </div>
        </div>
      )}
      <p className="text-sm text-muted-foreground">
        {clientName || "This application"} is requesting access to:
      </p>
      {scopes.length > 0 && (
        <ul className="list-disc pl-6 text-sm">
          {scopes.map((s) => (
            <li key={s}>{s}</li>
          ))}
        </ul>
      )}
      <form method="POST" action="/oauth/consent">
        <fieldset className="flex flex-col gap-4">
          <Button type="submit">Authorize</Button>
        </fieldset>
      </form>
    </div>
  );
}
