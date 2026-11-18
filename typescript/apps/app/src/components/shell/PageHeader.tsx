import type { ReactNode } from "react";
import { Separator, SidebarTrigger } from "internal/components/ui";

// PageHeader is the bar along the top of every page: the sidebar toggle,
// the page's title, and its actions on the right.
export function PageHeader({
  title,
  description,
  children,
}: {
  title: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <header className="sticky top-0 z-10 flex min-h-14 items-center gap-3 border-b bg-background/95 px-4 py-2 backdrop-blur-sm">
      <SidebarTrigger />
      <Separator orientation="vertical" className="h-5" />
      <div className="min-w-0 flex-1">
        <h1 className="truncate text-base font-semibold">{title}</h1>
        {description && (
          <p className="truncate text-xs text-muted-foreground">
            {description}
          </p>
        )}
      </div>
      {children && <div className="flex items-center gap-2">{children}</div>}
    </header>
  );
}

// PageBody is a page's scrolling content area.
export function PageBody({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={`mx-auto w-full max-w-5xl flex-1 p-6 ${className ?? ""}`}>
      {children}
    </div>
  );
}

// EmptyState fills a list that has nothing in it yet.
export function EmptyState({
  icon,
  title,
  children,
}: {
  icon?: ReactNode;
  title: string;
  children?: ReactNode;
}) {
  return (
    <div className="flex flex-col items-center gap-2 rounded-xl border border-dashed px-6 py-12 text-center">
      {icon && (
        <div className="text-muted-foreground [&_svg]:size-8">{icon}</div>
      )}
      <p className="font-medium">{title}</p>
      {children && (
        <div className="max-w-sm text-sm text-muted-foreground">{children}</div>
      )}
    </div>
  );
}
