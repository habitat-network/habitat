import { createFileRoute, redirect } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { Button, Card } from "internal/components/ui";
import { Search, Upload as UploadIcon } from "lucide-react";
import { useRef } from "react";
import { DropOverlay } from "@/components/DropOverlay";
import { FileTable } from "@/components/FileTable";
import { UploadTray } from "@/components/UploadTray";
import { useUploads } from "@/hooks/useUploads";
import { useWindowDrop } from "@/hooks/useWindowDrop";
import { formatBytes, MAX_FILE_BYTES } from "@/lib/files";
import { getCurrentOrg, listFiles } from "@/server/functions";

export const Route = createFileRoute("/_member/")({
  beforeLoad: async () => {
    // The parent's loader hasn't run yet when this does, so ask for the org
    // directly: no org selected means the picker.
    const currentOrg = await getCurrentOrg();
    if (!currentOrg) throw redirect({ to: "/orgs" });
    return { currentOrg };
  },
  // The first page of files comes through the loader (which crosses the SSR
  // boundary) and seeds the query below as initialData; after that the
  // query owns it, refetched whenever an upload lands and polled so other
  // members' uploads show up too.
  loader: async ({ context }) => ({
    currentOrg: context.currentOrg,
    files: await listFiles(),
  }),
  component: FilesPage,
});

// How often the file list is refetched. Synced rows land in D1 as space
// notifications and the cron trigger run passes, so there's nothing to push.
const FILES_POLL_MS = 10_000;

function FilesPage() {
  const { currentOrg, files: initialFiles } = Route.useLoaderData();
  const orgName = currentOrg.name ?? currentOrg.did;
  const { data: files = [] } = useQuery({
    queryKey: ["files", currentOrg.did],
    queryFn: () => listFiles(),
    initialData: initialFiles,
    refetchInterval: FILES_POLL_MS,
  });
  const { uploads, add, dismiss } = useUploads(currentOrg.did);
  const dragging = useWindowDrop(add);
  const input = useRef<HTMLInputElement>(null);

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-6 px-6 py-10">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-semibold tracking-tight">Files</h1>
          <p className="text-sm text-muted-foreground">
            Shared with everyone in {orgName} and indexed for search. Drop files
            anywhere on this page to upload.
          </p>
        </div>
        <Button onClick={() => input.current?.click()}>
          <UploadIcon data-icon="inline-start" />
          Upload files
        </Button>
        <input
          ref={input}
          type="file"
          multiple
          hidden
          onChange={(e) => {
            if (e.target.files) add(Array.from(e.target.files));
            e.target.value = "";
          }}
        />
      </div>

      <UploadTray uploads={uploads} onDismiss={dismiss} />

      {files.length === 0 ? (
        <EmptyState onBrowse={() => input.current?.click()} />
      ) : (
        <Card className="overflow-hidden p-0">
          <FileTable files={files} />
        </Card>
      )}

      <p className="flex items-center justify-center gap-1.5 text-xs text-muted-foreground">
        <Search className="size-3.5" />
        Up to {formatBytes(MAX_FILE_BYTES)} per file. Each file is stored in its
        own space that every member of {orgName} can read.
      </p>

      <DropOverlay visible={dragging} orgName={orgName} />
    </div>
  );
}

function EmptyState({ onBrowse }: { onBrowse: () => void }) {
  return (
    <button
      type="button"
      onClick={onBrowse}
      className="group flex flex-col items-center gap-4 rounded-3xl border-2 border-dashed border-border bg-card px-6 py-20 text-center transition-colors hover:border-primary/50 hover:bg-primary/[0.03]"
    >
      <span className="flex size-14 items-center justify-center rounded-2xl bg-primary/10 text-primary transition-transform group-hover:-translate-y-0.5">
        <UploadIcon className="size-6" />
      </span>
      <span className="flex flex-col gap-1">
        <span className="text-base font-medium">Drop files to share them</span>
        <span className="text-sm text-muted-foreground">
          Drag files anywhere onto this page, or click to browse.
        </span>
      </span>
    </button>
  );
}
