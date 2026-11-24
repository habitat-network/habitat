import { Link, useNavigate, useRouterState } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { OrgAvatar, UserAvatar } from "internal";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
} from "internal/components/ui";
import {
  AppWindow,
  Cable,
  ChevronsUpDown,
  FolderOpen,
  Home,
  LogIn,
  LogOut,
  Plus,
  Search,
  Settings,
  Shield,
  Plug,
  UserRound,
  Users,
  type LucideIcon,
} from "lucide-react";
import type { Member } from "@/server/fns/auth";
import { signOut } from "@/server/fns/auth";
import type { GroupSummary } from "@/server/fns/groups";
import { groupLabel, groupQuery } from "@/lib/queries";

// AppSidebar is the app's navigation: the group switcher, search, and the
// two sections of the design (Workspace and Manage are about the current
// group).
export function AppSidebar({
  member,
  groups,
  currentOrg,
}: {
  member: Member;
  groups: GroupSummary[];
  currentOrg: string | null;
}) {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const current = groups.find((g) => g.did === currentOrg) ?? null;
  const { data: group } = useQuery({
    ...groupQuery(currentOrg ?? ""),
    enabled: currentOrg !== null,
  });

  const groupItems: NavItem[] = currentOrg
    ? [
        { to: "/g/$org", label: "Home", icon: Home },
        { to: "/g/$org/files", label: "Files", icon: FolderOpen },
        { to: "/g/$org/search", label: "Search", icon: Search },
        // Apps is a dev-only section while it's being built.
        ...(import.meta.env.DEV
          ? [{ to: "/g/$org/apps" as const, label: "Apps", icon: AppWindow }]
          : []),
      ]
    : [];
  const manageItems: NavItem[] = currentOrg
    ? [
        {
          to: "/g/$org/members",
          label: "Members",
          icon: Users,
          badge: group?.memberCount,
        },
        { to: "/g/$org/roles", label: "Roles & access", icon: Shield },
        { to: "/g/$org/mcp", label: "MCP servers", icon: Plug },
        { to: "/g/$org/settings", label: "Group settings", icon: Settings },
      ]
    : [];

  return (
    <Sidebar variant="inset">
      <SidebarHeader>
        <GroupSwitcher groups={groups} current={current} />
      </SidebarHeader>
      <SidebarContent>
        {currentOrg && (
          <>
            <NavSection
              label="Workspace"
              org={currentOrg}
              items={groupItems}
              pathname={pathname}
            />
            <NavSection
              label="Manage"
              org={currentOrg}
              items={manageItems}
              pathname={pathname}
            />
          </>
        )}
      </SidebarContent>
      <SidebarFooter>
        <MemberCard
          member={member}
          org={currentOrg}
          displayName={group?.me.displayName ?? null}
          avatarUrl={group?.me.avatarUrl ?? null}
          roles={group?.myRoles ?? []}
        />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}

type GroupPath =
  | "/g/$org"
  | "/g/$org/files"
  | "/g/$org/search"
  | "/g/$org/apps"
  | "/g/$org/members"
  | "/g/$org/roles"
  | "/g/$org/mcp"
  | "/g/$org/settings";

interface NavItem {
  to: GroupPath;
  label: string;
  icon: LucideIcon;
  badge?: number;
}

function NavSection({
  label,
  org,
  items,
  pathname,
}: {
  label: string;
  org: string;
  items: NavItem[];
  pathname: string;
}) {
  const base = `/g/${encodeURIComponent(org)}`;
  return (
    <SidebarGroup>
      <SidebarGroupLabel>{label}</SidebarGroupLabel>
      <SidebarGroupContent>
        <SidebarMenu>
          {items.map((item) => {
            const href = item.to.replace("/g/$org", base);
            // The org's index route is Home; every other page starts with
            // its path, so Home is active only on the index itself.
            const active =
              (item.to === "/g/$org"
                ? pathname === base || pathname === `${base}/`
                : pathname.startsWith(href)) ||
              (item.to === "/g/$org/files" &&
                pathname.startsWith(`${base}/docs/`));
            return (
              <SidebarMenuItem key={item.to}>
                <SidebarMenuButton
                  isActive={active}
                  render={<Link to={item.to} params={{ org }} />}
                >
                  <item.icon />
                  <span>{item.label}</span>
                </SidebarMenuButton>
                {item.badge !== undefined && (
                  <SidebarMenuBadge>{item.badge}</SidebarMenuBadge>
                )}
              </SidebarMenuItem>
            );
          })}
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  );
}

// GroupSwitcher shows the current group and lists the member's others.
function GroupSwitcher({
  groups,
  current,
}: {
  groups: GroupSummary[];
  current: GroupSummary | null;
}) {
  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <SidebarMenuButton size="lg">
                {current ? (
                  <OrgAvatar
                    did={current.did}
                    name={current.name ?? undefined}
                  />
                ) : (
                  <div className="flex size-8 items-center justify-center rounded-full bg-muted">
                    <Users className="size-4" />
                  </div>
                )}
                <div className="grid flex-1 text-left leading-tight">
                  <span className="truncate font-semibold">
                    {current ? groupLabel(current) : "No group yet"}
                  </span>
                  <span className="truncate text-xs text-muted-foreground">
                    {groups.length === 1
                      ? "1 group"
                      : `${groups.length} groups`}
                  </span>
                </div>
                <ChevronsUpDown className="ml-auto" />
              </SidebarMenuButton>
            }
          />
          <DropdownMenuContent align="start" className="min-w-64">
            <DropdownMenuGroup>
              <DropdownMenuLabel>Your groups</DropdownMenuLabel>
              {groups.map((g) => (
                <DropdownMenuItem
                  key={g.did}
                  render={<Link to="/g/$org/files" params={{ org: g.did }} />}
                >
                  <OrgAvatar did={g.did} name={g.name ?? undefined} size="sm" />
                  <span className="truncate">{groupLabel(g)}</span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuItem render={<Link to="/join" />}>
              <LogIn />
              Join a group
            </DropdownMenuItem>
            <DropdownMenuItem render={<Link to="/groups/new" />}>
              <Plus />
              Create a group
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}

// MemberCard is the signed-in member, as the current group sees them.
function MemberCard({
  member,
  org,
  displayName,
  avatarUrl,
  roles,
}: {
  member: Member;
  org: string | null;
  displayName: string | null;
  avatarUrl: string | null;
  roles: string[];
}) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const actor = {
    did: member.did,
    handle: member.handle ?? undefined,
    displayName: displayName ?? undefined,
    avatar: avatarUrl ?? undefined,
  };
  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <SidebarMenuButton size="lg">
                <UserAvatar actor={actor} />
                <div className="grid flex-1 text-left leading-tight">
                  <span className="truncate font-medium">
                    {displayName || member.handle || member.did}
                  </span>
                  <span className="truncate text-xs text-muted-foreground">
                    {roles.length > 0
                      ? roles.join(", ")
                      : member.handle
                        ? `@${member.handle}`
                        : ""}
                  </span>
                </div>
                <ChevronsUpDown className="ml-auto" />
              </SidebarMenuButton>
            }
          />
          <DropdownMenuContent side="top" align="start" className="min-w-56">
            {org && (
              <DropdownMenuItem
                render={
                  <Link to="/g/$org/settings" params={{ org }} hash="profile" />
                }
              >
                <UserRound />
                Your profile in this group
              </DropdownMenuItem>
            )}
            <DropdownMenuItem render={<Link to="/connections" />}>
              <Cable />
              Connections
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={async () => {
                await signOut();
                queryClient.clear();
                await navigate({ to: "/login" });
              }}
            >
              <LogOut />
              Sign out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
