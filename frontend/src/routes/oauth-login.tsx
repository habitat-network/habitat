import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { AuthForm } from "internal";

const loginSearchSchema = z.object({
  handle: z.string().optional(),
});

export const Route = createFileRoute("/oauth-login")({
  validateSearch: loginSearchSchema,
  component() {
    const { handle } = Route.useSearch();
    const { authManager } = Route.useRouteContext();
    const error =
      new URLSearchParams(window.location.search).get("error") ?? undefined;
    return (
      <AuthForm
        authManager={authManager}
        redirectUrl={`https://${__DOMAIN__}`}
        serverError={error}
        defaultHandle={handle}
      />
    );
  },
});
