import { createFileRoute } from "@tanstack/react-router";
import { AuthForm } from "internal";

export const Route = createFileRoute("/login")({
  component() {
    const { authManager } = Route.useRouteContext();
    return (
      <>
        <h2>Welcome to greensky! Please login with your ATProtocol account (e.g. me.bsky.social) </h2>
        <AuthForm
          authManager={authManager}
          redirectUrl={`https://${__DOMAIN__}`}
        />
      </>

    );
  },
});
