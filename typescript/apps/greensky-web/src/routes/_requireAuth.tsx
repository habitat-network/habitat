import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import { AppLayout } from "internal";
import { postsQueryOptions } from "@/queries/posts";

export const Route = createFileRoute("/_requireAuth")({
  async beforeLoad({ context }) {
    await context.authManager.maybeExchangeCode();
    if (!context.authManager.getAuthInfo()) {
      throw redirect({ to: "/login" });
    }
  },
  loader({ context }) {
    const did = context.authManager.getAuthInfo()!.did;
    // Warm the feed before the component renders.
    void context.queryClient.prefetchQuery(
      postsQueryOptions(context.authManager),
    );
    return { did };
  },
  component() {
    const { did } = Route.useLoaderData();
    const { authManager } = Route.useRouteContext();
    return (
      <AppLayout
        actor={{ did }}
        title="Greensky"
        onSignOut={() => authManager.logout()}
      >
        <Outlet />
      </AppLayout>
    );
  },
});
