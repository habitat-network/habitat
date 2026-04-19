import { createFileRoute } from "@tanstack/react-router";

import { AuthForm } from "internal";

export const Route = createFileRoute("/oauth-login")({
  component() {
    const { authManager } = Route.useRouteContext();
    const error =
      new URLSearchParams(window.location.search).get("error") ?? undefined;
    return (
      <AuthForm
        authManager={authManager}
        redirectUrl={`https://${__DOMAIN__}`}
        serverError={error}
      />
    );
  },
});
