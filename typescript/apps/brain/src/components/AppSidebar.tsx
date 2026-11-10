import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "internal/components/ui";
import {
  ArrowLeftRightIcon,
  BrainIcon,
  PlusIcon,
  SettingsIcon,
} from "lucide-react";
import { listChats, type Member } from "@/server/functions";
import type { Org } from "@/server/habitat";
import { useOrgStore } from "@/stores/org";
import { UserMenu } from "./UserMenu";

export function AppSidebar({
  member,
  org,
}: {
  member: Member;
  org: Org | undefined;
}) {
  const selectOrg = useOrgStore((s) => s.selectOrg);
  const navigate = useNavigate();
  const { chat: activeChat } = useParams({ strict: false });
  const { data: chats = [] } = useQuery({
    queryKey: ["chats", org?.did],
    queryFn: () => listChats({ data: { org: org!.did } }),
    enabled: !!org,
  });
  // A new chat is just a fresh id: it is stored with its first message.
  const newChat = () =>
    navigate({ to: "/chat/$chat", params: { chat: crypto.randomUUID() } });

  return (
    <Sidebar>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" render={<Link to="/" />}>
              <BrainIcon />
              <span className="font-semibold">Brain</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
          {org && (
            <SidebarMenuItem>
              <SidebarMenuButton
                onClick={() => selectOrg(null)}
                tooltip="Switch organization"
              >
                <ArrowLeftRightIcon />
                <span className="truncate">{org.name}</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          )}
          <SidebarMenuItem>
            <SidebarMenuButton onClick={newChat} disabled={!org}>
              <PlusIcon />
              New chat
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>Chats</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {chats.map((chat) => (
                <SidebarMenuItem key={chat.id}>
                  <SidebarMenuButton
                    isActive={chat.id === activeChat}
                    render={
                      <Link to="/chat/$chat" params={{ chat: chat.id }} />
                    }
                  >
                    <span className="truncate">{chat.title}</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter>
        <SidebarMenu>
          {org && (
            <SidebarMenuItem>
              <SidebarMenuButton render={<Link to="/settings" />}>
                <SettingsIcon />
                Settings
              </SidebarMenuButton>
            </SidebarMenuItem>
          )}
        </SidebarMenu>
        <UserMenu member={member} />
      </SidebarFooter>
    </Sidebar>
  );
}
