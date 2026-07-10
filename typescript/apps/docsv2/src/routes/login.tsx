import { createFileRoute, useRouter } from "@tanstack/react-router";
import { useState } from "react";
import { Button, Input } from "internal";

export const Route = createFileRoute("/login")({
  component() {
    const { fetcher } = Route.useRouteContext();
    const router = useRouter();
    const [handle, setHandle] = useState("");

    const orgLoginUrl = router.buildLocation({ to: "/org-login" }).href;

    return (
      <div className="min-h-screen flex items-center justify-center p-4">
        <div className="w-full max-w-sm rounded-xl border bg-background p-8 shadow-sm">
          {/* Login is a top-level navigation to the docs server, which runs the
              OAuth flow via sap and hands back a server-session cookie. */}
          <form
            onSubmit={(e) => {
              e.preventDefault();
              const h = handle.trim();
              if (h) {
                window.location.href = fetcher.loginUrl(h);
              }
            }}
          >
            <div className="space-y-4">
              <Input
                value={handle}
                onChange={(e) => setHandle(e.target.value)}
                placeholder="alice.bsky.social"
              />
              <Button type="submit" className="w-full" disabled={!handle.trim()}>
                Sign In
              </Button>
            </div>
          </form>
          <Button
            variant="link"
            className="mt-6"
            size="sm"
            render={<a href={orgLoginUrl} />}
          >
            Add this app to your organization
          </Button>
        </div>
      </div>
    );
  },
});
