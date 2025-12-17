import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_requireAuth")({
  beforeLoad({ context }) {
    if (!context.authManager.isAuthenticated()) {
      throw redirect({ to: "/login" });
    }
  },
  component() {
    return <Outlet />;
  },
});
