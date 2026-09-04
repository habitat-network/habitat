import { Button, Field, FieldLabel, Input } from "internal/components/ui";
import { createFileRoute } from "@tanstack/react-router";
import { Unplug } from "lucide-react";

type OrgProfile = {
  name: string;
  description?: string;
};

type OpensocialLoginInfo = {
  orgProfile: OrgProfile;
  clientName: string;
  clientUri: string;
  logoUri: string;
};

// Opensocial org sign-in page. pear's authorization endpoint redirects here
// when the identity being signed into is an opensocial org, rather than
// starting a login provider directly. The admin enters their handle; the
// same endpoint verifies their admin membership and, on success, redirects
// on to their PDS to complete the login.
export const Route = createFileRoute("/login/opensocial")({
  loader: async (): Promise<OpensocialLoginInfo> => {
    const res = await fetch("/oauth/opensocial");
    if (!res.ok) throw new Error("Failed to load org profile");
    return (await res.json()) as OpensocialLoginInfo;
  },
  component: OpensocialLoginPage,
});

function OpensocialLoginPage() {
  const { orgProfile, clientName, clientUri, logoUri } = Route.useLoaderData();

  const clientInfo = clientName || clientUri;

  return (
    <div className="flex w-full max-w-md flex-col gap-4">
      <p>
        Approve {clientInfo} access spaces in {orgProfile.name}
      </p>
      <div className="flex flex-col items-center gap-2 text-center">
        <div className="flex min-w-0 flex-col items-center gap-1">
          {logoUri && <img src={logoUri} alt="" className="h-8 w-8 rounded" />}
          <p className="truncate font-medium">{clientName || clientUri}</p>
          {clientName && (
            <p className="truncate text-xs text-muted-foreground">
              {clientUri}
            </p>
          )}
        </div>
        <Unplug className="h-4 w-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0">
          <p className="truncate font-medium">{orgProfile.name}</p>
          {orgProfile.description && (
            <p className="truncate text-xs text-muted-foreground">
              {orgProfile.description}
            </p>
          )}
        </div>
      </div>
      <p>Sign in as an admin of {orgProfile.name} to continue.</p>
      <form method="POST" action="/oauth/opensocial">
        <fieldset className="flex flex-col gap-4">
          <Field>
            <FieldLabel>Handle</FieldLabel>
            <Input placeholder="handle" autoFocus name="handle" required />
          </Field>
          <Button type="submit">Continue</Button>
        </fieldset>
      </form>
    </div>
  );
}
