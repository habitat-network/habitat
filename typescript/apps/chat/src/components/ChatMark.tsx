import { cn } from "@/lib/cn";

// Chat's mark: a speech bubble, in the theme's primary color.
export function ChatMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 32 32"
      aria-hidden="true"
      className={cn("size-8 text-primary", className)}
    >
      <rect width="32" height="32" rx="9" fill="currentColor" />
      <path
        d="M9 10.5A2.5 2.5 0 0 1 11.5 8h9a2.5 2.5 0 0 1 2.5 2.5v6a2.5 2.5 0 0 1-2.5 2.5H15l-3.6 3.1a.5.5 0 0 1-.9-.4V19A2.5 2.5 0 0 1 9 16.5v-6Z"
        fill="var(--primary-foreground)"
      />
    </svg>
  );
}
