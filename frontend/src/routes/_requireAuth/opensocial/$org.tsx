import type { CSSProperties } from "react";
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
import { EditProfileDialog } from "@/components/EditProfileDialog";
import { OrgAvatar } from "internal";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
} from "internal/components/ui";
import { ensureValidDid } from "@atproto/syntax";
import {
  UsersIcon,
  ShieldIcon,
  PlugIcon,
  LayoutDashboardIcon,
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
    | "/opensocial/$org"
    | "/opensocial/$org/members"
    | "/opensocial/$org/roles"
    | "/opensocial/$org/apps";
  label: string;
  icon: typeof LayoutDashboardIcon;
  exact?: boolean;
}[] = [
  {
    to: "/opensocial/$org",
    label: "Overview",
    icon: LayoutDashboardIcon,
    exact: true,
  },
  { to: "/opensocial/$org/members", label: "Members", icon: UsersIcon },
  {
    to: "/opensocial/$org/roles",
    label: "Roles & capabilities",
    icon: ShieldIcon,
  },
  { to: "/opensocial/$org/apps", label: "Authorized apps", icon: PlugIcon },
];

function OrgLayout() {
  const { org } = Route.useParams();
  const { authManager } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const members = Route.useLoaderData();
  const { data: profile } = useQuery(
    orgProfileQueryOptions(org, authManager, queryClient),
  );
  const currentPath = useLocation({ select: (loc) => loc.pathname });

  // The caller is an admin if their own membership record (already loaded
  // as part of the member list) carries the admin role. Fine-grained gating
  // per action happens on the roles page and is enforced server-side by
  // every mutation regardless.
  const isAdmin = members.some(
    (m) =>
      m.did === authManager.getAuthInfo()?.did && m.roles.includes("admin"),
  );

  return (
    <div className="flex flex-col gap-4 py-6">
      <div>
        <Link
          to="/opensocial"
          className="text-sm text-muted-foreground hover:text-foreground"
        >
          ← All organizations
        </Link>
        <div className="flex items-center justify-between gap-3 mt-2">
          <div className="flex items-center gap-3">
            <OrgAvatar
              did={org}
              name={profile?.name}
              avatarUrl={profile?.avatarUrl}
              size="lg"
            />
            <h1 className="text-2xl font-semibold">
              {profile?.name ?? <DidHoverCard did={org}>{org}</DidHoverCard>}
            </h1>
          </div>
          {isAdmin && (
            <EditProfileDialog
              org={org}
              name={profile?.name ?? ""}
              description={profile?.description ?? ""}
              avatarUrl={profile?.avatarUrl}
              authManager={authManager}
            />
          )}
        </div>
        {profile?.description && (
          <p className="text-muted-foreground mt-1">{profile.description}</p>
        )}
        <p className="font-mono text-xs text-muted-foreground break-all mt-1">
          {org}
        </p>
      </div>

      <SidebarProvider
        className="min-h-0 items-start"
        style={{ "--sidebar-width": "14rem" } as CSSProperties}
      >
        <Sidebar collapsible="none" className="rounded-lg border bg-card">
          <SidebarContent>
            <SidebarGroup>
              <SidebarGroupContent>
                <SidebarMenu>
                  {NAV_ITEMS.map((item) => (
                    <SidebarMenuItem key={item.to}>
                      <SidebarMenuButton
                        isActive={
                          item.exact
                            ? currentPath === `/opensocial/${org}` ||
                              currentPath === `/opensocial/${org}/`
                            : currentPath.startsWith(
                                item.to.replace("$org", org),
                              )
                        }
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
        <SidebarInset className="bg-transparent">
          <Outlet />
        </SidebarInset>
      </SidebarProvider>
    </div>
  );
}
