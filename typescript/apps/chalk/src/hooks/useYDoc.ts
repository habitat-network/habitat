import { useEffect, useMemo } from "react";
import * as Y from "yjs";
import { WebsocketProvider } from "y-websocket";

// useYDoc owns a doc's Y.Doc for the lifetime of a docId: it creates a fresh
// one per docId and hands it to a WebsocketProvider connected to DocRoom
// (src/routes/ws.$docId.ts / src/server/rooms/docRoom.ts), which does both
// directions itself — applying remote merges as they arrive and sending
// local edits back — via the standard y-websocket/y-protocols sync
// protocol, including its own reconnect-with-backoff. Any editor wired to
// the returned Y.Doc gets live collaboration for free; nothing here is
// TipTap/ProseMirror-specific.
export function useYDoc(docId: string): Y.Doc {
  // docId isn't referenced in the factory itself — it's deliberately a reset
  // key, not a real dependency: a fresh Y.Doc per doc, discarding the
  // previous one the moment docId changes.
  // oxlint-disable-next-line react-hooks/exhaustive-deps
  const ydoc = useMemo(() => new Y.Doc(), [docId]);

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

  return ydoc;
}
