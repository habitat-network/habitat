import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Outlet, useParams } from "@tanstack/react-router";
import { SidebarInset, SidebarProvider } from "internal/components/ui";
import { useEffect, useSyncExternalStore } from "react";
import { AppSidebar } from "@/components/shell/AppSidebar";
import { myGroupsQuery } from "@/lib/queries/groups";
import { getMember } from "@/server/fns/auth";

const LAST_GROUP_KEY = "habitat.lastGroup";

// Layout for everything behind sign-in: beforeLoad redirects to /login when
// there is no session (getMember's NotSignedIn), and puts the signed-in
// member in route context. It renders the sidebar around every page.
export const Route = createFileRoute("/_member")({
  beforeLoad: async () => ({ member: await getMember() }),
  loader: ({ context }) => context.queryClient.ensureQueryData(myGroupsQuery),
  component: MemberLayout,
});

function MemberLayout() {
  const { member } = Route.useRouteContext();
  const { data: groups } = useSuspenseQuery(myGroupsQuery);
  const { org } = useParams({ strict: false });
  const lastGroup = useLastGroup(org);
  // Pages outside a group (Connections, Join, Create) keep the sidebar on
  // the group the member was last in.
  const currentOrg =
    org ??
    (groups.some((g) => g.did === lastGroup)
      ? lastGroup
      : (groups[0]?.did ?? null));
  return (
    <SidebarProvider>
      <AppSidebar member={member} groups={groups} currentOrg={currentOrg} />
      <SidebarInset className="min-h-svh">
        <Outlet />
      </SidebarInset>
    </SidebarProvider>
  );
}

// useLastGroup remembers the group the member last opened, per browser.
function useLastGroup(org: string | undefined): string | null {
  useEffect(() => {
    if (!org) return;
    try {
      localStorage.setItem(LAST_GROUP_KEY, org);
    } catch {
      // Storage can be unavailable (private mode); the sidebar then falls
      // back to the first group.
    }
  }, [org]);
  const stored = useSyncExternalStore(
    subscribeToStorage,
    readLastGroup,
    () => null,
  );
  return org ?? stored;
}

function subscribeToStorage(onChange: () => void) {
  window.addEventListener("storage", onChange);
  return () => window.removeEventListener("storage", onChange);
}

function readLastGroup(): string | null {
  try {
    return localStorage.getItem(LAST_GROUP_KEY);
  } catch {
    return null;
  }
}
