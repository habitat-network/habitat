import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import { getProfile } from "../habitatApi";

export const Route = createFileRoute("/_requireAuth")({
  async beforeLoad({ context }) {
    await context.authManager.maybeExchangeCode(window.location.href);
    if (!context.authManager.getAuthInfo()) {
      throw redirect({ to: "/login" });
    }
    const did = context.authManager.getAuthInfo()!.did;
    const myProfile = await getProfile(context.authManager, did);
    return { myProfile };
  },
  component() {
    return <Outlet />;
  },
});
