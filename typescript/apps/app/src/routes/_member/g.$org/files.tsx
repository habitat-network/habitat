import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { z } from "zod";
import {
  Button,
  Card,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  ToggleGroup,
  ToggleGroupItem,
  toast,
} from "internal/components/ui";
import {
  Download,
  FileText,
  Files,
  FolderOpen,
  LayoutGrid,
  List,
  MoreVertical,
  Plus,
  Trash2,
  Upload as UploadIcon,
  X,
} from "lucide-react";
import { useRef, type ReactNode } from "react";
import { DropOverlay } from "@/components/files/DropOverlay";
import { FileIcon } from "@/components/files/FileIcon";
import { UploadTray } from "@/components/files/UploadTray";
import { EmptyState, PageHeader } from "@/components/shell/PageHeader";
import { useUploads } from "@/hooks/useUploads";
import { useWindowDrop } from "@/hooks/useWindowDrop";
import { cn } from "@/lib/cn";
import { formatBytes, MAX_FILE_BYTES } from "@/lib/files";
import { groupLabel, groupQuery } from "@/lib/queries";
import { createDoc, listDocs, type DocSummary } from "@/server/fns/docs";
import { deleteFile, listFiles, type FileView } from "@/server/fns/files";

// Files is the group's workspace: its collaborative docs (from chalk) and
// uploaded files (from drop) in one list. The left column narrows by kind,
// the list shows as rows or a grid, and selecting an item opens its details.

const Search = z.object({
  kind: z.enum(["all", "docs", "uploads"]).optional(),
  view: z.enum(["list", "grid"]).optional(),
  selected: z.string().optional(),
});

export const Route = createFileRoute("/_member/g/$org/files")({
  validateSearch: Search,
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(docsQuery(params.org)),
      context.queryClient.ensureQueryData(filesQuery(params.org)),
    ]),
  component: FilesPage,
});

// Synced rows land in D1 as space notifications and cron passes arrive, so
// the lists are polled; there's nothing to push.
const POLL_MS = 10_000;

const docsQuery = (org: string) => ({
  queryKey: ["docs", org],
  queryFn: () => listDocs({ data: { org } }),
});
const filesQuery = (org: string) => ({
  queryKey: ["files", org],
  queryFn: () => listFiles({ data: { org } }),
});

type Item =
  | { kind: "doc"; id: string; name: string; doc: DocSummary }
  | { kind: "file"; id: string; name: string; file: FileView };

function FilesPage() {
  const { org } = Route.useParams();
  const search = Route.useSearch();
  const kind = search.kind ?? "all";
  const view = search.view ?? "list";
  const navigate = useNavigate({ from: Route.fullPath });
  const queryClient = useQueryClient();
  const { data: group } = useQuery(groupQuery(org));
  const { data: docs = [] } = useQuery({
    ...docsQuery(org),
    refetchInterval: POLL_MS,
  });
  const { data: files = [] } = useQuery({
    ...filesQuery(org),
    refetchInterval: POLL_MS,
  });
  const { uploads, add, dismiss } = useUploads(org);
  const input = useRef<HTMLInputElement>(null);
  const connected = group?.connected ?? false;
  // Dropping files only uploads once the group is connected.
  const dragging = useWindowDrop((f) => connected && add(f));
  const name = group ? groupLabel(group) : "";

  const newDoc = useMutation({
    mutationFn: () => createDoc({ data: { org } }),
    onSuccess: async ({ docId }) => {
      await queryClient.invalidateQueries({ queryKey: ["docs", org] });
      await navigate({ to: "/g/$org/docs/$docId", params: { org, docId } });
    },
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Couldn't create a doc",
        description: e.message,
      }),
  });

  const items: Item[] = [
    ...(kind === "uploads"
      ? []
      : docs.map((d): Item => ({
          kind: "doc",
          id: d.docId,
          name: d.title || "Untitled",
          doc: d,
        }))),
    ...(kind === "docs"
      ? []
      : files.map((f): Item => ({
          kind: "file",
          id: f.uri,
          name: f.name,
          file: f,
        }))),
  ];
  const selected = items.find((i) => i.id === search.selected) ?? null;
  const setSearch = (patch: z.infer<typeof Search>) =>
    navigate({ search: (s) => ({ ...s, ...patch }), replace: true });

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title="Files"
        description={name && `Shared with everyone in ${name}`}
      >
        <ToggleGroup
          value={[view]}
          onValueChange={(v: string[]) =>
            v[0] && setSearch({ view: v[0] as "list" | "grid" })
          }
          variant="outline"
          size="sm"
        >
          <ToggleGroupItem value="list" aria-label="List">
            <List />
          </ToggleGroupItem>
          <ToggleGroupItem value="grid" aria-label="Grid">
            <LayoutGrid />
          </ToggleGroupItem>
        </ToggleGroup>
        <Button
          variant="outline"
          disabled={!connected}
          onClick={() => input.current?.click()}
        >
          <UploadIcon data-icon="inline-start" />
          Upload
        </Button>
        <Button
          disabled={!connected || newDoc.isPending}
          onClick={() => newDoc.mutate()}
        >
          <Plus data-icon="inline-start" />
          New doc
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
      </PageHeader>

      <div className="flex min-h-0 flex-1">
        <nav className="hidden w-52 shrink-0 flex-col gap-1 border-r p-3 md:flex">
          <KindButton
            active={kind === "all"}
            onClick={() => setSearch({ kind: undefined })}
            icon={<FolderOpen />}
            count={docs.length + files.length}
          >
            All files
          </KindButton>
          <KindButton
            active={kind === "docs"}
            onClick={() => setSearch({ kind: "docs" })}
            icon={<FileText />}
            count={docs.length}
          >
            Docs
          </KindButton>
          <KindButton
            active={kind === "uploads"}
            onClick={() => setSearch({ kind: "uploads" })}
            icon={<Files />}
            count={files.length}
          >
            Uploads
          </KindButton>
        </nav>

        <div className="flex min-w-0 flex-1 flex-col gap-4 overflow-y-auto p-6">
          <UploadTray uploads={uploads} onDismiss={dismiss} />
          {items.length === 0 ? (
            <EmptyState icon={<UploadIcon />} title="Nothing here yet">
              {connected
                ? "Create a doc, or drag files anywhere onto this page to upload them."
                : "Docs and files can be added once the group is connected."}
            </EmptyState>
          ) : view === "list" ? (
            <Card className="overflow-hidden p-0">
              <ItemTable
                org={org}
                items={items}
                selected={selected?.id}
                onSelect={(id) => setSearch({ selected: id })}
              />
            </Card>
          ) : (
            <ItemGrid
              org={org}
              items={items}
              selected={selected?.id}
              onSelect={(id) => setSearch({ selected: id })}
            />
          )}
          <p className="text-center text-xs text-muted-foreground">
            Uploads are up to {formatBytes(MAX_FILE_BYTES)} each. Every member
            of the group can read them, and they're indexed for search.
          </p>
        </div>

        {selected && (
          <DetailsPanel
            org={org}
            item={selected}
            onClose={() => setSearch({ selected: undefined })}
          />
        )}
      </div>

      <DropOverlay visible={dragging && connected} orgName={name} />
    </div>
  );
}

function KindButton({
  active,
  onClick,
  icon,
  count,
  children,
}: {
  active: boolean;
  onClick: () => void;
  icon: ReactNode;
  count: number;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex items-center gap-2 rounded-lg px-3 py-2 text-left text-sm [&_svg]:size-4",
        active
          ? "bg-muted font-medium"
          : "text-muted-foreground hover:bg-muted/60",
      )}
    >
      {icon}
      <span className="flex-1">{children}</span>
      <span className="text-xs tabular-nums text-muted-foreground">
        {count}
      </span>
    </button>
  );
}

const dateFormat = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  year: "numeric",
});

export function downloadUrl(org: string, uri: string) {
  return `/api/download?${new URLSearchParams({ org, uri })}`;
}

function ItemIcon({ item, className }: { item: Item; className?: string }) {
  return item.kind === "doc" ? (
    <span
      className={cn(
        "flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary",
        className,
      )}
    >
      <FileText className="size-4" />
    </span>
  ) : (
    <FileIcon mimeType={item.file.mimeType} className={className} />
  );
}

// ItemLink opens a doc in the editor, or downloads a file.
function ItemLink({
  org,
  item,
  className,
  children,
}: {
  org: string;
  item: Item;
  className?: string;
  children: ReactNode;
}) {
  return item.kind === "doc" ? (
    <Link
      to="/g/$org/docs/$docId"
      params={{ org, docId: item.id }}
      className={className}
    >
      {children}
    </Link>
  ) : (
    <a href={downloadUrl(org, item.id)} className={className}>
      {children}
    </a>
  );
}

function ItemTable({
  org,
  items,
  selected,
  onSelect,
}: {
  org: string;
  items: Item[];
  selected: string | undefined;
  onSelect: (id: string) => void;
}) {
  return (
    <Table>
      <TableHeader className="bg-muted/40">
        <TableRow className="hover:bg-transparent">
          <TableHead className="w-full pl-5">Name</TableHead>
          <TableHead className="hidden sm:table-cell">Kind</TableHead>
          <TableHead className="hidden md:table-cell">Added by</TableHead>
          <TableHead className="hidden md:table-cell">Added</TableHead>
          <TableHead className="text-right">Size</TableHead>
          <TableHead className="w-10 pr-5">
            <span className="sr-only">Actions</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.map((item) => (
          <TableRow
            key={item.id}
            data-state={item.id === selected ? "selected" : undefined}
            className="group cursor-pointer"
            onClick={() => onSelect(item.id)}
          >
            <TableCell className="max-w-0 pl-5">
              <div className="flex min-w-0 items-center gap-3 py-1">
                <ItemIcon item={item} />
                <ItemLink
                  org={org}
                  item={item}
                  className="truncate font-medium hover:underline"
                >
                  {item.name}
                </ItemLink>
              </div>
            </TableCell>
            <TableCell className="hidden text-muted-foreground sm:table-cell">
              {item.kind === "doc" ? "Doc" : "Upload"}
            </TableCell>
            <TableCell className="hidden max-w-48 truncate text-muted-foreground md:table-cell">
              {item.kind === "file"
                ? (item.file.uploaderName ?? item.file.uploadedBy ?? "—")
                : "—"}
            </TableCell>
            <TableCell className="hidden whitespace-nowrap text-muted-foreground md:table-cell">
              {item.kind === "file"
                ? dateFormat.format(new Date(item.file.createdAt))
                : "—"}
            </TableCell>
            <TableCell className="text-right whitespace-nowrap text-muted-foreground tabular-nums">
              {item.kind === "file" ? formatBytes(item.file.size) : "—"}
            </TableCell>
            <TableCell
              className="pr-5 text-right"
              onClick={(e) => e.stopPropagation()}
            >
              <ItemMenu org={org} item={item} />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function ItemGrid({
  org,
  items,
  selected,
  onSelect,
}: {
  org: string;
  items: Item[];
  selected: string | undefined;
  onSelect: (id: string) => void;
}) {
  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(10rem,1fr))] gap-3">
      {items.map((item) => (
        <button
          key={item.id}
          type="button"
          onClick={() => onSelect(item.id)}
          onDoubleClick={() => {
            if (item.kind === "file")
              window.location.href = downloadUrl(org, item.id);
          }}
          className={cn(
            "flex flex-col items-center gap-3 rounded-xl border p-4 text-center hover:bg-muted/50",
            item.id === selected && "border-primary bg-primary/5",
          )}
        >
          <ItemIcon item={item} className="size-12" />
          <span className="line-clamp-2 w-full text-sm font-medium break-words">
            {item.name}
          </span>
          <span className="text-xs text-muted-foreground">
            {item.kind === "doc" ? "Doc" : formatBytes(item.file.size)}
          </span>
        </button>
      ))}
    </div>
  );
}

function ItemMenu({ org, item }: { org: string; item: Item }) {
  const queryClient = useQueryClient();
  const remove = useMutation({
    mutationFn: (uri: string) => deleteFile({ data: { org, uri } }),
    // The server has deleted the record and run the sync pass that drops its
    // row by now, so the refetch is what removes it from the list.
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["files", org] }),
    onError: (error) =>
      toast.add({
        type: "error",
        title: `Couldn't delete ${item.name}`,
        description: error.message,
      }),
  });
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`More actions for ${item.name}`}
          />
        }
      >
        <MoreVertical />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {item.kind === "doc" ? (
          <DropdownMenuItem
            render={
              <Link to="/g/$org/docs/$docId" params={{ org, docId: item.id }} />
            }
          >
            <FileText />
            Open
          </DropdownMenuItem>
        ) : (
          <>
            <DropdownMenuItem render={<a href={downloadUrl(org, item.id)} />}>
              <Download />
              Download
            </DropdownMenuItem>
            <DropdownMenuItem
              variant="destructive"
              onClick={() => remove.mutate(item.id)}
            >
              <Trash2 />
              Delete
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function DetailsPanel({
  org,
  item,
  onClose,
}: {
  org: string;
  item: Item;
  onClose: () => void;
}) {
  return (
    <aside className="hidden w-72 shrink-0 flex-col gap-5 overflow-y-auto border-l p-5 lg:flex">
      <div className="flex items-start justify-between gap-2">
        <ItemIcon item={item} className="size-12" />
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Close details"
          onClick={onClose}
        >
          <X />
        </Button>
      </div>
      <div>
        <h2 className="font-semibold break-words">{item.name}</h2>
        <p className="text-sm text-muted-foreground">
          {item.kind === "doc" ? "Collaborative doc" : item.file.mimeType}
        </p>
      </div>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
        {item.kind === "file" ? (
          <>
            <dt className="text-muted-foreground">Size</dt>
            <dd>{formatBytes(item.file.size)}</dd>
            <dt className="text-muted-foreground">Added</dt>
            <dd>{new Date(item.file.createdAt).toLocaleString()}</dd>
            <dt className="text-muted-foreground">Added by</dt>
            <dd className="truncate">
              {item.file.uploaderName ?? item.file.uploadedBy ?? "—"}
            </dd>
            <dt className="text-muted-foreground">Access</dt>
            <dd>Every member</dd>
          </>
        ) : (
          <>
            <dt className="text-muted-foreground">Owner</dt>
            <dd className="truncate">
              {item.doc.ownerDid === org ? "The group" : item.doc.ownerDid}
            </dd>
            <dt className="text-muted-foreground">Access</dt>
            <dd>Shared from the doc</dd>
          </>
        )}
      </dl>
      <ItemLink org={org} item={item}>
        <Button className="w-full" variant="outline" render={<span />}>
          {item.kind === "doc" ? (
            <FileText data-icon="inline-start" />
          ) : (
            <Download data-icon="inline-start" />
          )}
          {item.kind === "doc" ? "Open doc" : "Download"}
        </Button>
      </ItemLink>
      <p className="text-xs text-muted-foreground break-all">{item.id}</p>
    </aside>
  );
}
