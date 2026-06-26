import { createFileRoute } from "@tanstack/react-router";
import { AuthForm } from "internal";

export const Route = createFileRoute("/login")({
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
