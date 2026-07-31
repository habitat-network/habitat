import type { ReactNode } from "react";

export interface SpacesPageLayoutProps {
  breadcrumb?: ReactNode;
  // Pages titled with an identifier — a DID, an NSID, a record key — pass it
  // wrapped in `font-mono break-all` so it reads correctly and can wrap
  // mid-token rather than overflow the page.
  title: ReactNode;
  subtitle?: ReactNode;
  // The page body. Anything richer than a single line of subtitle text (an
  // extra metadata line, an action row) belongs here.
  children: ReactNode;
}

// SpacesPageLayout is the common frame for every page under /spaces:
// breadcrumb, title, subtitle, then content, with one set of spacing.
export function SpacesPageLayout({
  breadcrumb,
  title,
  subtitle,
  children,
}: SpacesPageLayoutProps) {
  return (
    <div className="flex flex-col gap-6 py-6">
      {breadcrumb}
      <div>
        <h1 className="text-2xl font-semibold">{title}</h1>
        {subtitle && (
          <div className="text-muted-foreground text-sm">{subtitle}</div>
        )}
      </div>
      {children}
    </div>
  );
}
