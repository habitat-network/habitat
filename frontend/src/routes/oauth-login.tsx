import { createFileRoute } from "@tanstack/react-router";

import AuthForm from "internal/AuthForm.tsx";

export const Route = createFileRoute("/oauth-login")({
  component() {
    const { authManager } = Route.useRouteContext();
    return (
      <AuthForm
        authManager={authManager}
        redirectUrl={`https://${__DOMAIN__}`}
      />
    );
  },
});
