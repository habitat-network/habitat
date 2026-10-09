import {
  Button,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "internal/components/ui";
import { Download } from "lucide-react";
import { formatBytes } from "@/lib/files";
import type { FileView } from "@/server/functions";
import { FileIcon } from "./FileIcon";

const dateFormat = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  year: "numeric",
});
const timeFormat = new Intl.DateTimeFormat(undefined, {
  hour: "numeric",
  minute: "2-digit",
});

function downloadUrl(uri: string) {
  return `/api/download?${new URLSearchParams({ uri })}`;
}

export function FileTable({ files }: { files: FileView[] }) {
  return (
    <Table>
      <TableHeader className="bg-muted/40">
        <TableRow className="hover:bg-transparent">
          <TableHead className="w-full pl-5">Name</TableHead>
          <TableHead className="hidden sm:table-cell">Uploaded by</TableHead>
          <TableHead className="hidden md:table-cell">Added</TableHead>
          <TableHead className="text-right whitespace-nowrap">Size</TableHead>
          <TableHead className="w-14 pr-5">
            <span className="sr-only">Download</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {files.map((file) => {
          const added = new Date(file.createdAt);
          return (
            <TableRow key={file.uri} className="group">
              <TableCell className="max-w-0 pl-5">
                <a
                  href={downloadUrl(file.uri)}
                  className="flex min-w-0 items-center gap-3 py-1"
                  title={file.name}
                >
                  <FileIcon mimeType={file.mimeType} />
                  <span className="truncate font-medium group-hover:underline">
                    {file.name}
                  </span>
                </a>
              </TableCell>
              <TableCell className="hidden max-w-56 truncate text-muted-foreground sm:table-cell">
                {file.uploaderHandle
                  ? `@${file.uploaderHandle}`
                  : (file.uploadedBy ?? "—")}
              </TableCell>
              <TableCell
                className="hidden whitespace-nowrap text-muted-foreground md:table-cell"
                title={added.toLocaleString()}
              >
                {dateFormat.format(added)}
                <span className="text-muted-foreground/60">
                  {" · "}
                  {timeFormat.format(added)}
                </span>
              </TableCell>
              <TableCell className="text-right whitespace-nowrap text-muted-foreground tabular-nums">
                {formatBytes(file.size)}
              </TableCell>
              <TableCell className="pr-5 text-right">
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Download ${file.name}`}
                  render={<a href={downloadUrl(file.uri)} />}
                >
                  <Download />
                </Button>
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}
