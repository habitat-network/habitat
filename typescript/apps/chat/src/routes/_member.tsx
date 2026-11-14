import { createFileRoute, Outlet } from "@tanstack/react-router";
import { AppHeader } from "@/components/AppHeader";
import { OrgSwitcher } from "@/components/OrgSwitcher";
import { getCurrentOrg, getMember } from "@/server/functions";

// Layout for everything behind sign-in: the header (logo, org switcher, user
// menu) over the page.
export const Route = createFileRoute("/_member")({
  beforeLoad: async () => ({ member: await getMember() }),
  loader: async ({ context }) => ({
    member: context.member,
    currentOrg: await getCurrentOrg(),
  }),
  component() {
    const { member, currentOrg } = Route.useLoaderData();
    return (
      <div className="flex min-h-svh flex-col bg-muted/30">
        <AppHeader member={member}>
          {currentOrg && <OrgSwitcher currentOrg={currentOrg} />}
        </AppHeader>
        <main className="flex-1">
          <Outlet />
        </main>
      </div>
    );
  },
});
