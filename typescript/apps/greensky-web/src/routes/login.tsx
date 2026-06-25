import { createFileRoute, Link } from "@tanstack/react-router";
import { AuthForm } from "internal";

export const Route = createFileRoute("/login")({
  component() {
    const { authManager } = Route.useRouteContext();
    return (
      <div className="flex flex-col items-center gap-6 py-10">
        <AuthForm
          authManager={authManager}
          redirectUrl={`https://${__DOMAIN__}`}
        />
        <Link to="/onboard" className="text-sm underline text-muted-foreground">
          Admin? Connect your organization to Greensky →
        </Link>
      </div>
    );
  },
});
