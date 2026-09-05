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
import { UserDisplayName } from "./UserDisplayName";

interface AppLayoutProps {
  actor?: Actor;
  title?: string;
  sidebarHeader?: ReactNode;
  sidebarContent?: ReactNode;
  // Rendered in the sidebar footer, above the user menu — e.g. an app's
  // current context (chalk's Personal/org indicator).
  footerExtra?: ReactNode;
  // Extra items appended to the user dropdown menu, after "Habitat Portal"
  // and before "Sign out" — e.g. chalk's "Switch org".
  dropdownMenuItems?: ReactNode;
  onSignOut?: () => void;
  children: ReactNode;
}

export const AppLayout = ({
  actor,
  sidebarContent,
  sidebarHeader,
  footerExtra,
  dropdownMenuItems,
  onSignOut,
  children,
}: AppLayoutProps) => {
  return (
    <SidebarProvider>
      <Sidebar collapsible="icon" variant="inset">
        <SidebarHeader>{sidebarHeader}</SidebarHeader>
        <SidebarContent>{sidebarContent}</SidebarContent>
        <SidebarFooter>
          {footerExtra}
          {actor && (
            <SidebarMenu>
              <SidebarMenuItem>
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={
                      <SidebarMenuButton size="lg">
                        <UserAvatar actor={actor} size="default" />
                        <span>
                          <UserDisplayName actor={actor} />
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
                    {dropdownMenuItems}
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
