import { createFileRoute } from "@tanstack/react-router";

import { AuthForm } from "internal";

export const Route = createFileRoute("/oauth-login")({
  validateSearch: (search) => {
    return {
      handle: search.handle as string,
    } satisfies {
      handle?: string;
    };
  },
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
