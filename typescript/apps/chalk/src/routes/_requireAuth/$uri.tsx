import { EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import Collaboration from "@tiptap/extension-collaboration";
import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { useEffect, useMemo, useRef, useState } from "react";
import * as Y from "yjs";
import { PageHeader } from "@/components/PageHeader";
import { HelpDialog } from "@/components/HelpDialog";
import {
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
  Button,
  Spinner,
} from "internal/components/ui";
import { CheckIcon } from "lucide-react";

// atob/btoa (not node's Buffer) since this runs in the browser.
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

// Preserve ydocs across navigations so the editor never flashes stale
// content; re-applying subscribeDoc's initial snapshot is safe since Yjs
// updates are idempotent.
const ydocRegistry = new Map<string, Y.Doc>();

// sendEdit/subscribeDoc live in functions.ts alongside plain (non
// createServerFn) exports — requireSession and subscribeDoc itself — that
// call useAppSession()/useSession() directly. A static top-level import
// here would pull that whole module, including those server-only calls,
// into the client bundle graph, which the build's import-protection plugin
// rejects. Dynamic imports inside each handler keep the reference inside
// the extracted server-only chunk instead (see the same note in
// ../_requireAuth.tsx). subscribeDoc itself is a plain async generator, so
// it's wrapped in a locally-defined streaming server function here rather
// than re-exported as one from functions.ts.
const sendEditFn = createServerFn({ method: "POST" })
  .validator((input: { docId: string; update: string }) => input)
  .handler(async ({ data }) => {
    const { sendEdit } = await import("@/server/functions");
    return sendEdit({ data });
  });

const subscribeDocFn = createServerFn({ method: "GET" })
  .validator((input: { docId: string }) => input)
  .handler(async function* ({ data }) {
    const { subscribeDoc } = await import("@/server/functions");
    yield* subscribeDoc(data);
  });

export const Route = createFileRoute("/_requireAuth/$uri")({
  loader: ({ params }) => {
    const docId = params.uri;
    const ydoc = ydocRegistry.get(docId) ?? new Y.Doc();
    ydocRegistry.set(docId, ydoc);
    return { docId, ydoc };
  },
  component() {
    const { docId, ydoc } = Route.useLoaderData();
    const [dirty, setDirty] = useState(false);
    // Guards against the feedback loop of re-sending an update Tiptap fires
    // onUpdate for because we just applied it ourselves from subscribeDoc.
    const applyingRemote = useRef(false);

    useEffect(() => {
      let cancelled = false;
      (async () => {
        try {
          const stream = await subscribeDocFn({ data: { docId } });
          for await (const { state } of stream) {
            if (cancelled) return;
            applyingRemote.current = true;
            try {
              Y.applyUpdateV2(ydoc, base64ToBytes(state));
            } finally {
              applyingRemote.current = false;
            }
          }
        } catch (e) {
          if (!cancelled) console.error("subscribeDoc stream ended", e);
        }
      })();
      return () => {
        cancelled = true;
      };
    }, [docId, ydoc]);

    const handleUpdate = useMemo(() => {
      let prevTimeout: number | undefined;
      return () => {
        if (applyingRemote.current) return; // don't re-send what we just received
        setDirty(true);
        clearTimeout(prevTimeout);
        prevTimeout = window.setTimeout(async () => {
          try {
            await sendEditFn({
              data: {
                docId,
                update: bytesToBase64(Y.encodeStateAsUpdateV2(ydoc)),
              },
            });
            setDirty(false);
          } catch (e) {
            console.error("failed to save doc", e);
          }
        }, 1000);
      };
    }, [docId, ydoc]);

    const editor = useEditor(
      {
        extensions: [
          StarterKit.configure({ undoRedo: false }),
          Collaboration.configure({ document: ydoc }),
        ],
        editorProps: {
          attributes: {
            class:
              "prose max-w-none min-h-full px-[max(2rem,calc(50%-22.5rem))] py-10 outline-none",
          },
        },
        onUpdate: handleUpdate,
      },
      [ydoc],
    );

    return (
      <div className="flex flex-col-reverse h-full">
        <div className="flex-1 flex flex-col items-center">
          <EditorContent className="w-full flex-1" editor={editor} />
        </div>
        <PageHeader>
          <div className="flex items-center gap-2">
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
  pendingComponent: () => <article>Loading...</article>,
});
