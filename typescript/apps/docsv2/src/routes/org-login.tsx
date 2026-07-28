import { DidResolver } from "@atproto/identity";
import { createFileRoute } from "@tanstack/react-router";
import { Button, Input } from "internal";

export const Route = createFileRoute("/org-login")({
  async loader() {
    const didres = new DidResolver({});
    const doc = await didres.resolve(
      import.meta.env.VITE_DOCS_SERVER_DID || "",
    );
    // The docs server advertises itself under the #docs service fragment; its
    // endpoint is where the /org/login bootstrap lives.
    const endpoint = doc?.service?.find((s) =>
      s.id.endsWith("#docs"),
    )?.serviceEndpoint;
    return { server: typeof endpoint === "string" ? endpoint : undefined };
  },
  component() {
    const { server } = Route.useLoaderData();
    return (
      <div className="min-h-screen flex items-center justify-center p-4">
        <div className="w-full max-w-sm space-y-4 rounded-xl border bg-background p-8 shadow-sm">
          <div className="space-y-2 text-center">
            <h1 className="text-lg font-medium">Authorize Habitat Docs</h1>
            <p className="text-sm text-muted-foreground">
              As an org admin, enter your org handle to grant the docs server
              access so it can manage documents on everyone's behalf. You'll be
              sent to Habitat to approve.
            </p>
          </div>
          {server ? (
            // A GET form navigates the browser to <docs-server>/org/login?handle=…
            // (a top-level navigation, so no CORS), which kicks off sap's OAuth.
            <form
              action={`${server}/org/login`}
              method="get"
              className="space-y-4"
            >
              <Input
                name="handle"
                placeholder="acmecorp.pear.local.habitat.network"
                required
              />
              <Button type="submit" className="w-full">
                Authorize for your org
              </Button>
            </form>
          ) : (
            <small className="text-destructive">
              Could not resolve the docs server.
            </small>
          )}
        </div>
      </div>
    );
  },
});
