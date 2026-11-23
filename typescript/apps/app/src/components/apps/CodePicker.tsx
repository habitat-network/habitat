import { Button } from "internal/components/ui";
import { FileCode, FolderUp, Upload } from "lucide-react";
import { useRef } from "react";
import { EXAMPLE_MODULES } from "@/lib/appExamples";
import { formatBytes } from "@/lib/files";
import {
  guessEntrypoint,
  isModulePath,
  normalizePath,
  stripCommonRoot,
} from "@/lib/apps";

export interface PickedCode {
  modules: Record<string, string>;
  entrypoint: string;
  // Files left out of the upload: unsupported types, or dependencies.
  skipped: string[];
}

// Paths an upload leaves out even when their type is supported: installed
// dependencies and hidden files. An app's code must be bundled already.
function ignored(path: string): boolean {
  return path.split("/").some((p) => p === "node_modules" || p.startsWith("."));
}

// readPicked reads the chosen files into a bundle's modules.
export async function readPicked(files: File[]): Promise<PickedCode> {
  const entries = files.flatMap((file) => {
    const path = normalizePath(file.webkitRelativePath || file.name);
    return path ? [{ file, path }] : [];
  });
  const paths = stripCommonRoot(entries.map((e) => e.path));
  const modules: Record<string, string> = {};
  const skipped: string[] = [];
  for (const [i, entry] of entries.entries()) {
    const path = paths[i];
    if (ignored(path) || !isModulePath(path)) {
      skipped.push(path);
      continue;
    }
    modules[path] = await entry.file.text();
  }
  return {
    modules,
    entrypoint: guessEntrypoint(Object.keys(modules)) ?? "",
    skipped,
  };
}

// CodePicker chooses an app's code: files, a folder, or the example, and
// which of its ES modules is the entrypoint.
export function CodePicker({
  value,
  onChange,
}: {
  value: PickedCode | null;
  onChange: (code: PickedCode) => void;
}) {
  const filesInput = useRef<HTMLInputElement>(null);
  const folderInput = useRef<HTMLInputElement>(null);
  const pick = async (list: FileList | null) => {
    if (!list || list.length === 0) return;
    onChange(await readPicked([...list]));
  };
  const paths = value ? Object.keys(value.modules).sort() : [];
  const size = value
    ? Object.values(value.modules).reduce(
        (n, s) => n + new TextEncoder().encode(s).byteLength,
        0,
      )
    : 0;
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap gap-2">
        <Button
          type="button"
          variant="outline"
          onClick={() => filesInput.current?.click()}
        >
          <Upload data-icon="inline-start" />
          Choose files
        </Button>
        <Button
          type="button"
          variant="outline"
          onClick={() => folderInput.current?.click()}
        >
          <FolderUp data-icon="inline-start" />
          Choose folder
        </Button>
        <Button
          type="button"
          variant="ghost"
          onClick={() =>
            onChange({
              modules: EXAMPLE_MODULES,
              entrypoint: "index.js",
              skipped: [],
            })
          }
        >
          <FileCode data-icon="inline-start" />
          Use the example
        </Button>
        <input
          ref={filesInput}
          type="file"
          multiple
          hidden
          onChange={(e) => {
            void pick(e.target.files);
            e.target.value = "";
          }}
        />
        <input
          ref={folderInput}
          type="file"
          hidden
          // Not in React's input props, but every current browser supports
          // picking a folder this way.
          {...({ webkitdirectory: "" } as object)}
          onChange={(e) => {
            void pick(e.target.files);
            e.target.value = "";
          }}
        />
      </div>
      {value && (
        <div className="rounded-lg border">
          <div className="flex items-center justify-between border-b px-3 py-2 text-xs text-muted-foreground">
            <span>
              {paths.length} {paths.length === 1 ? "file" : "files"},{" "}
              {formatBytes(size)}. Pick the entrypoint.
            </span>
            {value.skipped.length > 0 && (
              <span title={value.skipped.join("\n")}>
                {value.skipped.length} skipped
              </span>
            )}
          </div>
          {paths.length === 0 ? (
            <p className="px-3 py-2 text-xs text-muted-foreground">
              None of those files can be deployed. Upload .js or .mjs modules,
              with any .json, .html, .css or .txt files they import.
            </p>
          ) : (
            <ul className="max-h-48 overflow-y-auto py-1 font-mono text-xs">
              {paths.map((p) => {
                const canEnter = /\.m?js$/i.test(p);
                return (
                  <li key={p} className="px-3 py-1">
                    <label className="flex items-center gap-2">
                      <input
                        type="radio"
                        name="entrypoint"
                        disabled={!canEnter}
                        checked={value.entrypoint === p}
                        onChange={() => onChange({ ...value, entrypoint: p })}
                      />
                      <span
                        className={
                          canEnter ? "flex-1" : "flex-1 text-muted-foreground"
                        }
                      >
                        {p}
                      </span>
                      {value.entrypoint === p && (
                        <span className="text-muted-foreground">
                          entrypoint
                        </span>
                      )}
                    </label>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
