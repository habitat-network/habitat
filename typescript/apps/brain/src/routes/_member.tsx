import { createFileRoute, Outlet } from "@tanstack/react-router";
import {
  SidebarInset,
  SidebarProvider,
  SidebarTrigger,
} from "internal/components/ui";
import { AppSidebar } from "@/components/AppSidebar";
import { OrgPicker } from "@/components/OrgPicker";
import { getMember, listOrgs } from "@/server/functions";
import { useOrgStore } from "@/stores/org";

// Layout for everything behind sign-in: the chat list beside the page, once
// an org is selected (until then, the org picker).
export const Route = createFileRoute("/_member")({
  beforeLoad: async () => ({
    member: await getMember(),
    orgs: await listOrgs(),
  }),
  component() {
    const { member, orgs } = Route.useRouteContext();
    const selectedOrg = useOrgStore((s) => s.selectedOrg);
    // A stored selection the member has since left counts as none.
    const org = orgs.find((o) => o.did === selectedOrg);
    return (
      <SidebarProvider>
        <AppSidebar member={member} org={org} />
        <SidebarInset>
          <header className="flex h-12 items-center px-3">
            <SidebarTrigger />
          </header>
          {org ? <Outlet /> : <OrgPicker orgs={orgs} />}
        </SidebarInset>
      </SidebarProvider>
    );
  },
});
