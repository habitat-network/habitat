import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { LoginForm } from "internal";

const loginSearchSchema = z.object({
  handle: z.string().optional(),
  domain: z.string().optional(),
});

export const Route = createFileRoute("/oauth-login")({
  validateSearch: loginSearchSchema,
  component() {
    const { handle, domain } = Route.useSearch();
    const { authManager } = Route.useRouteContext();
    const error =
      new URLSearchParams(window.location.search).get("error") ?? undefined;
    return (
      <LoginForm
        authManager={authManager}
        redirectUrl={`https://${__DOMAIN__}`}
        defaultDomain={__HABITAT_DOMAIN__}
        serverError={error}
        defaultHandle={handle}
        customDomain={domain}
      />
    );
  },
});
