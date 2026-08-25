import { ReactNode } from "react";
import { Actor } from "@/types/Actor";

import { HabitatLogo } from "./HabitatLogo";
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
  useSidebar,
  SidebarRail,
} from "./ui/sidebar";
import { LogOut } from "lucide-react";
import { UserItem } from "./UserItem";

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
  title,
  sidebarContent: sidebarContent,
  sidebarHeader,
  onSignOut,
  children,
}: AppLayoutProps) => {
  return (
    <SidebarProvider>
      <Sidebar collapsible="icon" variant="inset">
        <SidebarHeader>
          {sidebarHeader}
        </SidebarHeader>
        <SidebarContent>{sidebarContent}</SidebarContent>
        <SidebarFooter>
          {actor && (
            <SidebarMenu>
              <SidebarMenuItem>
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={
                      <SidebarMenuButton size="lg">
                        <UserItem actor={actor} />
                      </SidebarMenuButton>
                    }
                  ></DropdownMenuTrigger>
                  <DropdownMenuContent align="start" side="right">
                    <DropdownMenuItem
                      render={
                        <a
                          href="https://habitat.network/habitat"
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
        <SidebarRail />
      </Sidebar>
      <SidebarInset>{children}</SidebarInset>
    </SidebarProvider>
  );
};
