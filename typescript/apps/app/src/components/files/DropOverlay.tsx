import { Upload } from "lucide-react";
import { cn } from "@/lib/cn";

// DropOverlay covers the window while files are dragged over it, so it's
// obvious a drop anywhere will upload.
export function DropOverlay({
  visible,
  orgName,
}: {
  visible: boolean;
  orgName: string;
}) {
  return (
    <div
      aria-hidden={!visible}
      className={cn(
        "pointer-events-none fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-6 backdrop-blur-sm transition-opacity duration-150",
        visible ? "opacity-100" : "opacity-0",
      )}
    >
      <div className="flex h-full w-full flex-col items-center justify-center gap-4 rounded-[2rem] border-2 border-dashed border-primary bg-primary/5">
        <span className="flex size-16 items-center justify-center rounded-2xl bg-primary text-primary-foreground shadow-lg">
          <Upload className="size-7" />
        </span>
        <div className="flex flex-col items-center gap-1 text-center">
          <p className="text-lg font-semibold">Drop to upload</p>
          <p className="text-sm text-muted-foreground">
            Files will be shared with everyone in {orgName}
          </p>
        </div>
      </div>
    </div>
  );
}
