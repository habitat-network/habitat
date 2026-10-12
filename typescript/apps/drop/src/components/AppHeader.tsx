import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import type { Member } from "@/server/functions";
import { DropMark } from "./DropMark";
import { UserMenu } from "./UserMenu";

export function AppHeader({
  member,
  children,
}: {
  member: Member;
  // Rendered in the top-right corner, before the user menu.
  children?: ReactNode;
}) {
  return (
    <header className="sticky top-0 z-30 border-b border-border/70 bg-background/85 backdrop-blur supports-[backdrop-filter]:bg-background/70">
      <div className="mx-auto flex h-16 max-w-5xl items-center justify-between gap-4 px-6">
        <Link to="/" className="flex items-center gap-2.5">
          <DropMark className="size-7" />
          <span className="text-[15px] font-semibold tracking-tight">Drop</span>
        </Link>
        <div className="flex items-center gap-3">
          {children}
          <UserMenu member={member} />
        </div>
      </div>
    </header>
  );
}
