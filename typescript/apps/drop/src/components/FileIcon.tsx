import {
  FileArchive,
  FileAudio,
  FileCode,
  FileImage,
  FileSpreadsheet,
  FileText,
  FileVideo,
  File as FileGeneric,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@/lib/cn";

const BY_PREFIX: [string, LucideIcon, string][] = [
  ["image/", FileImage, "bg-sky-500/10 text-sky-600"],
  ["video/", FileVideo, "bg-violet-500/10 text-violet-600"],
  ["audio/", FileAudio, "bg-pink-500/10 text-pink-600"],
  ["text/csv", FileSpreadsheet, "bg-emerald-500/10 text-emerald-600"],
  [
    "application/vnd.ms-excel",
    FileSpreadsheet,
    "bg-emerald-500/10 text-emerald-600",
  ],
  [
    "application/vnd.openxmlformats-officedocument.spreadsheetml",
    FileSpreadsheet,
    "bg-emerald-500/10 text-emerald-600",
  ],
  ["application/zip", FileArchive, "bg-amber-500/10 text-amber-600"],
  ["application/gzip", FileArchive, "bg-amber-500/10 text-amber-600"],
  ["application/x-tar", FileArchive, "bg-amber-500/10 text-amber-600"],
  ["application/json", FileCode, "bg-slate-500/10 text-slate-600"],
  ["application/javascript", FileCode, "bg-slate-500/10 text-slate-600"],
  ["text/html", FileCode, "bg-slate-500/10 text-slate-600"],
  ["application/pdf", FileText, "bg-red-500/10 text-red-600"],
  ["text/", FileText, "bg-stone-500/10 text-stone-600"],
  ["application/msword", FileText, "bg-blue-500/10 text-blue-600"],
  [
    "application/vnd.openxmlformats-officedocument.wordprocessingml",
    FileText,
    "bg-blue-500/10 text-blue-600",
  ],
];

export function FileIcon({
  mimeType,
  className,
}: {
  mimeType: string;
  className?: string;
}) {
  const match = BY_PREFIX.find(([prefix]) => mimeType.startsWith(prefix));
  const [, Icon, tone] = match ?? [
    "",
    FileGeneric,
    "bg-muted text-muted-foreground",
  ];
  return (
    <span
      className={cn(
        "flex size-9 shrink-0 items-center justify-center rounded-lg",
        tone,
        className,
      )}
    >
      <Icon className="size-4.5" />
    </span>
  );
}
