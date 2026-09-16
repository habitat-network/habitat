import {
  createFileRoute,
  Link,
  Outlet,
  useLocation,
} from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  orgMembersQueryOptions,
  orgProfileQueryOptions,
} from "@/queries/opensocial";
import { DidHoverCard } from "@/components/DidHoverCard";
import { OrgAvatar } from "internal";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
} from "internal/components/ui";
import { ensureValidDid } from "@atproto/syntax";
import {
  UsersIcon,
  ShieldIcon,
  KeyRoundIcon,
  PlugIcon,
  SettingsIcon,
} from "lucide-react";

export const Route = createFileRoute("/_requireAuth/opensocial/$org")({
  params: {
    parse: ({ org }) => {
      ensureValidDid(org);
      return { org };
    },
  },
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      orgMembersQueryOptions(
        params.org,
        context.authManager,
        context.queryClient,
      ),
    ),
  component: OrgLayout,
});

const NAV_ITEMS: {
  to:
    | "/opensocial/$org/members"
    | "/opensocial/$org/roles"
    | "/opensocial/$org/capabilities"
    | "/opensocial/$org/apps"
    | "/opensocial/$org/settings";
  label: string;
  icon: typeof UsersIcon;
}[] = [
  { to: "/opensocial/$org/members", label: "Members", icon: UsersIcon },
  { to: "/opensocial/$org/roles", label: "Roles", icon: ShieldIcon },
  {
    to: "/opensocial/$org/capabilities",
    label: "Capabilities",
    icon: KeyRoundIcon,
  },
  { to: "/opensocial/$org/apps", label: "Authorized apps", icon: PlugIcon },
  { to: "/opensocial/$org/settings", label: "Settings", icon: SettingsIcon },
];

function OrgLayout() {
  const { org } = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const { data: profile } = useQuery(
    orgProfileQueryOptions(org, authManager, queryClient),
  );
  const currentPath = useLocation({ select: (loc) => loc.pathname });

  return (
    // [contain:layout] gives this element its own containing block, so the
    // sidebar's `fixed` positioning is scoped to this section instead of the
    // real viewport — otherwise it would render fixed to the browser window
    // and overlap the app's own top nav bar (rendered above this route in
    // __root.tsx), which isn't itself fixed/sticky.
    <SidebarProvider className="min-h-[calc(100svh-1px)] [contain:layout]">
      <Sidebar collapsible="icon" variant="floating">
        <SidebarHeader>
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton size="lg" render={<Link to="/opensocial" />}>
                <OrgAvatar
                  did={org}
                  name={profile?.name}
                  avatarUrl={profile?.avatarUrl}
                />
                <div className="flex flex-col overflow-hidden">
                  <span className="truncate font-medium">
                    {profile?.name ?? (
                      <DidHoverCard did={org}>{org}</DidHoverCard>
                    )}
                  </span>
                  <span className="truncate text-xs text-muted-foreground">
                    All organizations
                  </span>
                </div>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarHeader>
        <SidebarContent>
          <SidebarGroup>
            <SidebarGroupContent>
              <SidebarMenu>
                {NAV_ITEMS.map((item) => (
                  <SidebarMenuItem key={item.to}>
                    <SidebarMenuButton
                      isActive={currentPath.startsWith(
                        item.to.replace("$org", org),
                      )}
                      tooltip={item.label}
                      render={<Link to={item.to} params={{ org }} />}
                    >
                      <item.icon />
                      <span>{item.label}</span>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        </SidebarContent>
      </Sidebar>
      <SidebarInset>
        <div className="flex flex-col gap-6 py-6 px-4">
          <div className="flex items-center gap-2">
            <SidebarTrigger />
          </div>
          <Outlet />
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}
