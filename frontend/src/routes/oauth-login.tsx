import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { AuthForm } from "internal";

export const Route = createFileRoute("/oauth-login")({
  validateSearch: z.object({
    handle: z.string().optional(),
    error: z.string().optional(),
  }),
  component() {
    const { handle, error } = Route.useSearch();
    const { authManager } = Route.useRouteContext();
    return (
      <AuthForm
        authManager={authManager}
        serverError={error}
        defaultHandle={handle}
      />
    );
  },
});
