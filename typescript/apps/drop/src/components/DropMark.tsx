import { cn } from "@/lib/cn";

// Drop's mark: a droplet over a tray, in the theme's primary color.
export function DropMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 32 32"
      aria-hidden="true"
      className={cn("size-8 text-primary", className)}
    >
      <rect width="32" height="32" rx="9" fill="currentColor" />
      <path
        d="M16 6.5c-.3 0-.6.2-.8.4-1.6 2-5.2 6.8-5.2 10.1a6 6 0 0 0 12 0c0-3.3-3.6-8.1-5.2-10.1a1 1 0 0 0-.8-.4Z"
        fill="var(--primary-foreground)"
      />
      <path
        d="M8 24.5h16"
        stroke="var(--primary-foreground)"
        strokeWidth="2"
        strokeLinecap="round"
        opacity=".55"
      />
    </svg>
  );
}
