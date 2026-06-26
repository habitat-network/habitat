import { MenuIcon } from "lucide-react";
import { Button, useSidebar } from "internal/components/ui";
import { useIsMobile } from "node_modules/internal/src/components/hooks/use-mobile";

export function PageHeader({ children }: { children?: React.ReactNode }) {
  const { toggleSidebar } = useSidebar();
  const isMobile = useIsMobile();

  return (
    <header className="px-4 py-2 border-b flex justify-between items-center sticky top-0 bg-background/95 backdrop-blur-sm z-10">
      {isMobile && (
        <Button onClick={toggleSidebar} size="icon" variant="ghost">
          <MenuIcon />
        </Button>
      )}
      {children}
    </header>
  );
}
