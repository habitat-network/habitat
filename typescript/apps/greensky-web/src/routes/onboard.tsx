import { createFileRoute, Link } from "@tanstack/react-router";
import { useState } from "react";
import { Button, Input } from "internal";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "internal/components/ui";

// greenskyServerOrigin derives the server's HTTPS origin from its did:web,
// e.g. did:web:greensky-server.local.habitat.network -> https://...network.
function greenskyServerOrigin(): string {
  const id = __GREENSKY_SERVER_DID__.replace(/^did:web:/, "");
  const host = decodeURIComponent(id.split(":")[0]);
  return `https://${host}`;
}

export const Route = createFileRoute("/onboard")({
  component() {
    const [handle, setHandle] = useState("");

    const onSubmit = (e: React.FormEvent) => {
      e.preventDefault();
      const trimmed = handle.trim();
      if (!trimmed) return;
      // Top-level navigation to the greensky server's /org/add, which calls
      // sap and 303-redirects through pear's OAuth flow to grant the org
      // credential. A plain navigation keeps this cross-origin step CORS-free.
      window.location.href = `${greenskyServerOrigin()}/org/add?handle=${encodeURIComponent(
        trimmed,
      )}`;
    };

    return (
      <div className="flex justify-center py-10 px-4">
        <Card className="w-full max-w-md">
          <CardHeader>
            <CardTitle>Connect your organization</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground mb-4">
              Enter your organization's handle. Greensky will ask you to sign in
              as an admin to authorize it to read your org's posts.
            </p>
            <form onSubmit={onSubmit} className="flex flex-col gap-3">
              <Input
                placeholder="myorg.local.habitat.network"
                value={handle}
                onChange={(e) => setHandle(e.target.value)}
                autoFocus
              />
              <Button type="submit" disabled={!handle.trim()}>
                Connect organization
              </Button>
            </form>
            <Link
              to="/login"
              className="text-sm underline text-muted-foreground mt-4 inline-block"
            >
              ← Back to sign in
            </Link>
          </CardContent>
        </Card>
      </div>
    );
  },
});
