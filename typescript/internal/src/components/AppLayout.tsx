import { ReactNode } from "react";
import { Actor } from "@/types/Actor";

import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "./ui/dropdown-menu";
import {
  Sidebar,
  SidebarProvider,
  SidebarHeader,
  SidebarContent,
  SidebarFooter,
  SidebarInset,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
} from "./ui/sidebar";
import { LogOut } from "lucide-react";
import { UserAvatar } from "./UserAvatar";

interface AppLayoutProps {
  actor?: Actor;
  title?: string;
  sidebarHeader?: ReactNode;
  sidebarContent?: ReactNode;
  onSignOut?: () => void;
  children: ReactNode;
}

export const AppLayout = ({
  actor,
  sidebarContent,
  sidebarHeader,
  onSignOut,
  children,
}: AppLayoutProps) => {
  return (
    <SidebarProvider>
      <Sidebar collapsible="icon" variant="inset">
        <SidebarHeader>{sidebarHeader}</SidebarHeader>
        <SidebarContent>{sidebarContent}</SidebarContent>
        <SidebarFooter>
          {actor && (
            <SidebarMenu>
              <SidebarMenuItem>
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={
                      <SidebarMenuButton size="lg">
                        <UserAvatar actor={actor} size="default" />
                        <span>
                          {actor.displayName || actor.handle || actor.did}
                        </span>
                      </SidebarMenuButton>
                    }
                  ></DropdownMenuTrigger>
                  <DropdownMenuContent align="start" side="right">
                    <DropdownMenuItem
                      render={
                        <a
                          href="https://home.habitat.network"
                          target="_blank"
                        />
                      }
                    >
                      <p>🌱</p>
                      Habitat Portal
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      onClick={onSignOut}
                      className="text-destructive"
                    >
                      <LogOut />
                      Sign out
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </SidebarMenuItem>
            </SidebarMenu>
          )}
        </SidebarFooter>
      </Sidebar>
      <SidebarInset>{children}</SidebarInset>
    </SidebarProvider>
  );
};
