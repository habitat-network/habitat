import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import { DidResolver } from "@atproto/identity";
import { getProfile } from "internal";

export const Route = createFileRoute("/_requireAuth")({
  async beforeLoad({ context }) {
    await context.authManager.maybeExchangeCode();
    if (!context.authManager.getAuthInfo()) {
      throw redirect({ to: "/login" });
    }
    const did = context.authManager.getAuthInfo()!.did;
    const [myProfile, didDoc] = await Promise.all([
      getProfile(did),
      new DidResolver({}).resolve(did),
    ]);
    const habitatServiceKey = import.meta.env.DEV ? "habitat_local" : "habitat";
    const isOnboarded =
      didDoc?.service?.some(
        (s: { id: string; type: string }) =>
          s.id === `#${habitatServiceKey}` && s.type === "HabitatServer",
      ) ?? false;
    return { myProfile, isOnboarded };
  },
  component() {
    return <Outlet />;
  },
});
