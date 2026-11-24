import { Plug } from "lucide-react";
import { cn } from "@/lib/cn";

// McpServerIcon is a server's brand mark, or a plug for servers without one.
export function McpServerIcon({
  icon,
  className,
}: {
  icon: string | null;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "flex size-8 shrink-0 items-center justify-center rounded-lg bg-muted",
        className,
      )}
    >
      {icon ? (
        <img src={icon} alt="" className="size-4" />
      ) : (
        <Plug className="size-4" />
      )}
    </span>
  );
}
