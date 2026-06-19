import { createFileRoute } from "@tanstack/react-router";
import { LoginForm } from "internal";

export const Route = createFileRoute("/login")({
  component() {
    const { authManager } = Route.useRouteContext();
    return (
      <LoginForm
        authManager={authManager}
        redirectUrl={`https://${__DOMAIN__}`}
        defaultDomain={__HABITAT_DOMAIN__}
      />
    );
  },
});
