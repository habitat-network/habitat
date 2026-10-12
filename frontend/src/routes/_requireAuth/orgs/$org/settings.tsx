import {
  createFileRoute,
  Link,
  Outlet,
  useMatchRoute,
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
import {
  UsersIcon,
  ShieldIcon,
  KeyRoundIcon,
  PlugIcon,
  ServerIcon,
  PaletteIcon,
  SearchIcon,
} from "lucide-react";

export const Route = createFileRoute("/_requireAuth/orgs/$org/settings")({
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
    | "/orgs/$org/settings/members"
    | "/orgs/$org/settings/roles"
    | "/orgs/$org/settings/capabilities"
    | "/orgs/$org/settings/apps"
    | "/orgs/$org/settings/mcp"
    | "/orgs/$org/settings/search"
    | "/orgs/$org/settings/branding";
  label: string;
  icon: typeof UsersIcon;
}[] = [
  { to: "/orgs/$org/settings/members", label: "Members", icon: UsersIcon },
  { to: "/orgs/$org/settings/roles", label: "Roles", icon: ShieldIcon },
  {
    to: "/orgs/$org/settings/capabilities",
    label: "Capabilities",
    icon: KeyRoundIcon,
  },
  { to: "/orgs/$org/settings/apps", label: "Authorized apps", icon: PlugIcon },
  { to: "/orgs/$org/settings/mcp", label: "MCP servers", icon: ServerIcon },
  { to: "/orgs/$org/settings/search", label: "Search", icon: SearchIcon },
  { to: "/orgs/$org/settings/branding", label: "Branding", icon: PaletteIcon },
];

function OrgLayout() {
  const { org } = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const { data: profile } = useQuery(
    orgProfileQueryOptions(org, authManager, queryClient),
  );
  const matchRoute = useMatchRoute();

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
              <SidebarMenuButton size="lg" render={<Link to="/orgs" />}>
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
                    Settings
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
                      isActive={
                        !!matchRoute({
                          to: item.to,
                          params: { org },
                          fuzzy: true,
                        })
                      }
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
