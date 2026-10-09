import { Button, Card, Spinner } from "internal/components/ui";
import { CircleAlert, X } from "lucide-react";
import { formatBytes } from "@/lib/files";
import type { Upload } from "@/hooks/useUploads";

// UploadTray lists files still on their way in: their upload progress, then
// a "syncing" state while the server creates the file's space and waits for
// the syncer to pick it up.
export function UploadTray({
  uploads,
  onDismiss,
}: {
  uploads: Upload[];
  onDismiss: (id: string) => void;
}) {
  if (uploads.length === 0) return null;
  return (
    <Card className="gap-0 divide-y divide-border p-0">
      {uploads.map((u) => (
        <div key={u.id} className="flex items-center gap-3 px-5 py-3">
          {u.status === "error" ? (
            <CircleAlert className="size-4 shrink-0 text-destructive" />
          ) : (
            <Spinner className="size-4 shrink-0 text-muted-foreground" />
          )}
          <div className="flex min-w-0 flex-1 flex-col gap-1.5">
            <div className="flex items-baseline justify-between gap-3 text-sm">
              <span className="truncate font-medium">{u.name}</span>
              <span className="shrink-0 text-xs text-muted-foreground">
                {u.status === "uploading" &&
                  `${Math.round(u.progress * 100)}% of ${formatBytes(u.size)}`}
                {u.status === "processing" && "Syncing…"}
                {u.status === "error" && (
                  <span className="text-destructive">{u.error}</span>
                )}
              </span>
            </div>
            {u.status !== "error" && (
              <div className="h-1 overflow-hidden rounded-full bg-muted">
                <div
                  className={
                    u.status === "processing"
                      ? "h-full w-full animate-pulse rounded-full bg-primary/60"
                      : "h-full rounded-full bg-primary transition-[width] duration-200"
                  }
                  style={
                    u.status === "uploading"
                      ? { width: `${Math.max(2, u.progress * 100)}%` }
                      : undefined
                  }
                />
              </div>
            )}
          </div>
          {u.status === "error" && (
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label="Dismiss"
              onClick={() => onDismiss(u.id)}
            >
              <X />
            </Button>
          )}
        </div>
      ))}
    </Card>
  );
}
