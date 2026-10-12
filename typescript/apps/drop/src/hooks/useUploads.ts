import { useCallback, useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "internal/components/ui";
import { MAX_FILE_BYTES, formatBytes } from "@/lib/files";

export interface Upload {
  id: string;
  name: string;
  size: number;
  // 0..1 while the bytes are going up; the server then still has to create
  // the space, write the record and wait for the sync pass.
  progress: number;
  status: "uploading" | "processing" | "error";
  error?: string;
}

// At most this many uploads run at once; the rest wait their turn.
const CONCURRENCY = 3;

// useUploads runs files through POST /api/upload, tracking per-file progress
// for the upload tray. A file disappears from the tray once the server has
// synced it (the list refetch picks it up), or stays with its error.
export function useUploads(orgDid: string) {
  const queryClient = useQueryClient();
  const [uploads, setUploads] = useState<Upload[]>([]);
  const queue = useRef<{ id: string; file: File }[]>([]);
  const active = useRef(0);

  const update = useCallback(
    (id: string, patch: Partial<Upload>) =>
      setUploads((all) =>
        all.map((u) => (u.id === id ? { ...u, ...patch } : u)),
      ),
    [],
  );
  const remove = useCallback(
    (id: string) => setUploads((all) => all.filter((u) => u.id !== id)),
    [],
  );

  // pump starts queued uploads until CONCURRENCY are running; each finished
  // upload calls it again (through the ref, so the callback doesn't capture
  // itself) to start the next.
  const pumpRef = useRef<() => void>(() => {});
  const pump = useCallback(() => {
    while (active.current < CONCURRENCY && queue.current.length > 0) {
      const next = queue.current.shift();
      if (!next) break;
      active.current++;
      send(next.id, next.file, update)
        .then(async ({ syncError }) => {
          if (syncError) {
            // Stored in the org, but the first sync pass failed, so it isn't
            // listed yet. Say so rather than let it look like a clean upload.
            toast.add({
              type: "warning",
              title: `${next.file.name} was uploaded but hasn't synced`,
              description: syncError,
            });
          }
          await queryClient.invalidateQueries({ queryKey: ["files", orgDid] });
          remove(next.id);
        })
        .catch((err: Error) => {
          update(next.id, { status: "error", error: err.message });
          toast.add({
            type: "error",
            title: `Couldn't upload ${next.file.name}`,
            description: err.message,
          });
        })
        .finally(() => {
          active.current--;
          pumpRef.current();
        });
    }
  }, [orgDid, queryClient, remove, update]);
  useEffect(() => {
    pumpRef.current = pump;
  }, [pump]);

  const add = useCallback(
    (files: Iterable<File>) => {
      const accepted: Upload[] = [];
      for (const file of files) {
        if (file.size > MAX_FILE_BYTES) {
          toast.add({
            type: "error",
            title: `${file.name} is too large`,
            description: `Files can be at most ${formatBytes(MAX_FILE_BYTES)}; this one is ${formatBytes(file.size)}.`,
          });
          continue;
        }
        const id = crypto.randomUUID();
        accepted.push({
          id,
          name: file.name,
          size: file.size,
          progress: 0,
          status: "uploading",
        });
        queue.current.push({ id, file });
      }
      if (accepted.length === 0) return;
      setUploads((all) => [...accepted, ...all]);
      pump();
    },
    [pump],
  );

  return { uploads, add, dismiss: remove };
}

// send uploads one file with XMLHttpRequest rather than fetch, which has no
// upload progress events.
function send(
  id: string,
  file: File,
  update: (id: string, patch: Partial<Upload>) => void,
): Promise<{ syncError?: string }> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", `/api/upload?${new URLSearchParams({ name: file.name })}`);
    xhr.setRequestHeader(
      "content-type",
      file.type || "application/octet-stream",
    );
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) update(id, { progress: e.loaded / e.total });
    };
    xhr.upload.onload = () => update(id, { progress: 1, status: "processing" });
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          return resolve(
            JSON.parse(xhr.responseText) as { syncError?: string },
          );
        } catch {
          return resolve({});
        }
      }
      let message = `Upload failed (${xhr.status})`;
      try {
        message =
          (JSON.parse(xhr.responseText) as { message?: string }).message ??
          message;
      } catch {
        // Not JSON; keep the generic message.
      }
      reject(new Error(message));
    };
    xhr.onerror = () => reject(new Error("Network error"));
    xhr.send(file);
  });
}
