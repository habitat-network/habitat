import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
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
  toast,
} from "internal/components/ui";
import { BrainIcon, PlusIcon } from "lucide-react";
import { createChat, listChats, type Member } from "@/server/functions";
import { UserMenu } from "./UserMenu";

// How often the chat list is refetched. Chats land in D1 as the syncer runs
// its passes, so there's nothing to push.
const CHATS_POLL_MS = 5_000;

export function AppSidebar({ member }: { member: Member }) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { space: activeSpace } = useParams({ strict: false });
  const { data: chats = [] } = useQuery({
    queryKey: ["chats"],
    queryFn: () => listChats(),
    refetchInterval: CHATS_POLL_MS,
  });
  const { mutate: newChat, isPending } = useMutation({
    mutationFn: () => createChat(),
    onSuccess: async ({ space }) => {
      await queryClient.invalidateQueries({ queryKey: ["chats"] });
      await navigate({ to: "/chat/$space", params: { space } });
    },
    onError: (error) =>
      toast.add({
        type: "error",
        title: "Couldn't start a chat",
        description: error.message,
      }),
  });

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
          <SidebarMenuItem>
            <SidebarMenuButton disabled={isPending} onClick={() => newChat()}>
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
                <SidebarMenuItem key={chat.space}>
                  <SidebarMenuButton
                    isActive={chat.space === activeSpace}
                    render={
                      <Link to="/chat/$space" params={{ space: chat.space }} />
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
        <UserMenu member={member} />
      </SidebarFooter>
    </Sidebar>
  );
}
