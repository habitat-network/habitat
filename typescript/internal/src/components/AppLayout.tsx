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
  SidebarGroup,
} from "./ui/sidebar";
import { LogOut } from "lucide-react";
import { UserItem } from "./UserItem";

interface AppLayoutProps {
  actor?: Actor;
  title?: string;
  sidebar?: ReactNode;
  onSignOut?: () => void;
  children: ReactNode;
}

function SidebarLogoButton({ title }: { title?: string }) {
  const { toggleSidebar } = useSidebar();
  return (
    <SidebarMenuButton size="lg" onClick={toggleSidebar}>
      <HabitatLogo />
      {title && <span className="font-semibold">{title}</span>}
    </SidebarMenuButton>
  );
}

export const AppLayout = ({
  actor,
  title,
  sidebar,
  onSignOut,
  children,
}: AppLayoutProps) => {
  return (
    <SidebarProvider>
      <Sidebar collapsible="icon">
        <SidebarHeader>
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarLogoButton title={title} />
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarHeader>
        <SidebarContent>{sidebar}</SidebarContent>
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
                  <DropdownMenuContent align="start" side="top">
                    <DropdownMenuItem
                      render={<a href="https://habitat.network/habitat" />}
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
