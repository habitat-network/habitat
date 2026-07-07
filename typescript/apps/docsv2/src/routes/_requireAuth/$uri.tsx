import { EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import { createFileRoute } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import Collaboration from "@tiptap/extension-collaboration";
import * as Y from "yjs";
import { useQuery } from "@tanstack/react-query";
import {
  docQueryOptions,
  docsListQueryOptions,
  pushUpdate,
} from "@/queries/docs";
import { ShareDialogV2, XRPCError } from "internal";
import {
  Button,
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
  Spinner,
} from "internal/components/ui";
import { HelpDialog } from "@/components/HelpDialog";
import { PageHeader } from "@/components/PageHeader";
import { CheckIcon } from "lucide-react";

// Preserve ydocs across navigations so the editor never flashes stale content.
// Re-applying the backend blob on each load is safe — Yjs updates are idempotent.
const ydocRegistry = new Map<string, Y.Doc>();

function base64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) {
    out[i] = bin.charCodeAt(i);
  }
  return out;
}

function bytesToBase64(bytes: Uint8Array): string {
  let bin = "";
  for (const b of bytes) {
    bin += String.fromCharCode(b);
  }
  return btoa(bin);
}

export const Route = createFileRoute("/_requireAuth/$uri")({
  async loader({ context, params }) {
    const docId = params.uri;
    const existing = ydocRegistry.get(docId);
    const ydoc = existing ?? new Y.Doc();
    if (!existing) {
      ydocRegistry.set(docId, ydoc);
    }

    // Re-apply the canonical state; Yjs CRDT merges are idempotent so this picks
    // up changes from other clients without clobbering local edits.
    const value = await context.queryClient.fetchQuery(
      docQueryOptions(docId, context.authManager),
    );
    if (value.blob) {
      Y.applyUpdateV2(ydoc, base64ToBytes(value.blob));
    }

    return { ydoc, docId };
  },
  preloadStaleTime: 1000 * 60 * 60,
  component() {
    const { ydoc, docId } = Route.useLoaderData();
    const { authManager } = Route.useRouteContext();
    const [dirty, setDirty] = useState(false);

    // The doc's space URI comes from the docs list; it's what ShareDialogV2
    // manages access for.
    const { data: docs } = useQuery(docsListQueryOptions(authManager));
    const spaceUri = docs?.find((d) => d.docId === docId)?.uri;

    // Debounced save: encode the full Yjs state and push it through pear to the
    // docs server, which merges it into the canonical record.
    const handleUpdate = useMemo(() => {
      let prevTimeout: number | undefined;
      return () => {
        setDirty(true);
        clearTimeout(prevTimeout);
        prevTimeout = window.setTimeout(async () => {
          try {
            await pushUpdate(
              authManager,
              docId,
              bytesToBase64(Y.encodeStateAsUpdateV2(ydoc)),
            );
            setDirty(false);
          } catch (e) {
            console.error("failed to save doc", e);
          }
        }, 1000);
      };
    }, [authManager, docId, ydoc]);

    const editor = useEditor(
      {
        extensions: [
          StarterKit.configure({ undoRedo: false }),
          Collaboration.configure({ document: ydoc }),
        ],
        editorProps: {
          attributes: {
            class:
              "prose max-w-none min-h-full px-[max(2rem,calc(50%-22.5rem))] py-10 outline-none selection:bg-[#d4edda]",
          },
        },
        onUpdate: handleUpdate,
      },
      [ydoc],
    );

    return (
      <div className="flex flex-col-reverse h-full">
        <div className="flex-1 flex flex-col items-center [&_.ProseMirror]:focus-visible:outline-2 [&_.ProseMirror]:focus-visible:outline-offset-[-1px] [&_.ProseMirror]:focus-visible:outline-ring/40">
          <EditorContent className="w-full flex-1" editor={editor} />
        </div>
        <PageHeader>
          <div className="flex items-center gap-2">
            {spaceUri && (
              <ShareDialogV2 spaceUri={spaceUri} authManager={authManager} />
            )}
            <Popover>
              <PopoverTrigger
                render={
                  <Button size="icon" variant="outline">
                    {dirty ? <Spinner /> : <CheckIcon />}
                  </Button>
                }
              />
              <PopoverContent>
                <PopoverTitle>Sync status</PopoverTitle>
                <span>{dirty ? "🔄 Syncing" : "✅ Synced"}</span>
              </PopoverContent>
            </Popover>
          </div>
          <HelpDialog />
        </PageHeader>
      </div>
    );
  },
  errorComponent({ error }) {
    if (error instanceof XRPCError && error.status === 403) {
      return <p>You do not have access to this doc</p>;
    }
    console.error(error);
    return <p>Something went wrong.</p>;
  },
  pendingComponent: () => <article>Loading...</article>,
});
