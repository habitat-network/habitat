import { useEffect, useMemo } from "react";
import * as Y from "yjs";
import { sendEdit, subscribeDoc } from "@/server/functions";

// Tags a Y.Doc transaction as one applied from a remote subscribeDoc update,
// so the ydoc "updateV2" listener below can tell it apart from a local edit
// (Collaboration's ProseMirror sync plugin tags its own transactions with
// its own origin, which is never this sentinel) and skip re-sending what
// was just received back to the server.
const REMOTE_ORIGIN = Symbol("remote");

// useYDoc owns a doc's Y.Doc for the lifetime of a docId: it creates a fresh
// one per docId, subscribes to subscribeDoc's stream to apply remote merges
// as they arrive, and sends every local edit back to the server as an
// incremental Yjs update (not a full-document snapshot — see sendEdit's
// caller below) via the ydoc's own "updateV2" event rather than an editor
// callback, so it works with any editor wired to the returned Y.Doc.
export function useYDoc(docId: string): Y.Doc {
  // docId isn't referenced in the factory itself — it's deliberately a reset
  // key, not a real dependency: a fresh Y.Doc per doc, discarding the
  // previous one the moment docId changes.
  // oxlint-disable-next-line react-hooks/exhaustive-deps
  const ydoc = useMemo(() => new Y.Doc(), [docId]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const stream = await subscribeDoc({ data: { docId } });
        for await (const { state } of stream) {
          if (cancelled) return;
          Y.applyUpdateV2(ydoc, state, REMOTE_ORIGIN);
        }
      } catch (e) {
        if (!cancelled) console.error("subscribeDoc stream ended", e);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [docId, ydoc]);

  useEffect(() => {
    // Listening on the Y.Doc directly (rather than an editor's own update
    // callback) gives us the update as an incremental diff of just this
    // transaction, not a snapshot of the whole document — encoding and
    // sending the full document on every keystroke would make each
    // request's size scale with document length instead of edit size.
    //
    // "updateV2", not "update": Y.Doc fires both, with the same payload
    // encoded in Yjs's two different wire formats (V1 on "update", V2 on
    // "updateV2"). Everywhere else in this app — Y.applyUpdateV2 above and
    // in docSync.ts/docStore.ts, Y.encodeStateAsUpdateV2 for persistence —
    // uses V2, so sending V1 bytes here would hand the server a buffer its
    // V2 decoder misreads as having a garbage length and throws on.
    const handleUpdate = (update: Uint8Array, origin: unknown) => {
      if (origin === REMOTE_ORIGIN) return;
      void sendEdit({ data: { docId, update } }).catch((e: unknown) => {
        console.error("failed to save doc", e);
      });
    };
    ydoc.on("updateV2", handleUpdate);
    return () => {
      ydoc.off("updateV2", handleUpdate);
    };
  }, [docId, ydoc]);

  return ydoc;
}
