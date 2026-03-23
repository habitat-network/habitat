import { MenuIcon } from "lucide-react";
import { Button, useSidebar } from "internal/components/ui";
import { useIsMobile } from "node_modules/internal/src/components/hooks/use-mobile";

export function PageHeader({ children }: { children?: React.ReactNode }) {
  const { toggleSidebar } = useSidebar();
  const isMobile = useIsMobile();

  return (
    <header className="px-3 py-1 border-b flex justify-between sticky top-0 bg-background">
      {isMobile && (
        <Button onClick={toggleSidebar} size="icon" variant="ghost">
          <MenuIcon />
        </Button>
      )}
      {children}
    </header>
  );
}
