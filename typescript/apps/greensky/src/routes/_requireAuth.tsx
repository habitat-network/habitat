import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_requireAuth")({
  async beforeLoad({ context }) {
    await context.authManager.maybeExchangeCode(window.location.href);
    if (!context.authManager.isAuthenticated()) {
      throw redirect({ to: "/login" });
    }
  },
  component() {
    return <Outlet />;
  },
});
