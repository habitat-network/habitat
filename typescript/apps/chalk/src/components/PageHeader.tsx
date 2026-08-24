import { MenuIcon } from "lucide-react";
import { Button, SidebarTrigger, useSidebar } from "internal/components/ui";
import { useIsMobile } from "internal/hooks";

export function PageHeader({ children }: { children?: React.ReactNode }) {
  return (
    <header className="px-4 py-2 border-b flex justify-between items-center sticky top-0 bg-background/95 backdrop-blur-sm z-10">
      <SidebarTrigger />
      {children}
    </header>
  );
}
