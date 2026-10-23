import { createFileRoute, Outlet } from "@tanstack/react-router";
import {
  SidebarInset,
  SidebarProvider,
  SidebarTrigger,
} from "internal/components/ui";
import { AppSidebar } from "@/components/AppSidebar";
import { getMember } from "@/server/functions";

// Layout for everything behind sign-in: the chat list beside the page.
export const Route = createFileRoute("/_member")({
  beforeLoad: async () => ({ member: await getMember() }),
  component() {
    const { member } = Route.useRouteContext();
    return (
      <SidebarProvider>
        <AppSidebar member={member} />
        <SidebarInset>
          <header className="flex h-12 items-center px-3">
            <SidebarTrigger />
          </header>
          <Outlet />
        </SidebarInset>
      </SidebarProvider>
    );
  },
});
