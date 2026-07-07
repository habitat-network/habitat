import { createFileRoute, useRouter } from "@tanstack/react-router";
import { AuthForm } from "internal";

export const Route = createFileRoute("/login")({
  component() {
    const { authManager } = Route.useRouteContext();
    const router = useRouter();
    return (
      <AuthForm
        authManager={authManager}
        redirectUrl={`https://${__DOMAIN__}`}
        orgLoginUrl={
          router.buildLocation({
            to: "/org-login",
          }).href
        }
      />
    );
  },
});
