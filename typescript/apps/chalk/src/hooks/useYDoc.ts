import { useEffect, useMemo } from "react";
import * as Y from "yjs";
import { WebsocketProvider } from "y-websocket";
import { useQueryClient } from "@tanstack/react-query";
import { computeTitle } from "@/render";
import type { DocSummary } from "@/db";

// useYDoc owns a doc's Y.Doc for the lifetime of a docId: it creates a fresh
// one per docId and hands it to a WebsocketProvider connected to DocRoom
// (src/routes/ws.$docId.ts / src/server/rooms/docRoom.ts), which does both
// directions itself — applying remote merges as they arrive and sending
// local edits back — via the standard y-websocket/y-protocols sync
// protocol, including its own reconnect-with-backoff. Any editor wired to
// the returned Y.Doc gets live collaboration for free; nothing here is
// TipTap/ProseMirror-specific.
//
// initialState, when given, seeds the Y.Doc synchronously on creation (see
// $uri.tsx's loader) so the editor renders with real content immediately
// instead of blank until the WebsocketProvider's own handshake completes.
export function useYDoc(docId: string, initialState?: Uint8Array): Y.Doc {
  // docId isn't referenced in the factory itself — it's deliberately a reset
  // key, not a real dependency: a fresh Y.Doc per doc, discarding the
  const queryClient = useQueryClient();
  const ydoc = useMemo(() => {
    const _ = docId; // so that it's included in the dependency list
    const doc = new Y.Doc();
    if (initialState) Y.applyUpdateV2(doc, initialState);
    return doc;
  }, [docId, initialState]);

  useEffect(() => {
    // wss:// (not ws://) whenever the page itself is https — a mixed-scheme
    // WebSocket from an https origin is blocked by the browser outright.
    const wsProtocol = location.protocol === "https:" ? "wss:" : "ws:";
    const provider = new WebsocketProvider(
      `${wsProtocol}//${location.host}/ws`,
      encodeURIComponent(docId),
      ydoc,
    );
    return () => {
      provider.destroy();
    };
  }, [docId, ydoc]);

  useEffect(() => {
    // Keep the sidebar/home page title optimistically in sync as the user
    // types, rather than waiting on docRoom's own debounced
    // republish-then-upsertDoc round-trip (up to ~10s). Debounced the same
    // 2s the server side settles on (docRoom.ts's IDLE_MS) — every ydoc
    // update resets the timer, so this fires 2s after edits (local or
    // remote) pause, not on every one.
    let timeout: ReturnType<typeof setTimeout> | undefined;
    const flushTitle = () => {
      const title = computeTitle(ydoc);
      queryClient.setQueryData<DocSummary[]>(["docs"], (old) =>
        old?.map((doc) => (doc.docId === docId ? { ...doc, title } : doc)),
      );
    };
    const onUpdate = () => {
      clearTimeout(timeout);
      timeout = setTimeout(flushTitle, 2000);
    };
    ydoc.on("update", onUpdate);
    return () => {
      clearTimeout(timeout);
      ydoc.off("update", onUpdate);
    };
  }, [ydoc, docId, queryClient]);

  return ydoc;
}
