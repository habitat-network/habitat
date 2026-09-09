import { Extension } from "@tiptap/core";
import { Plugin, PluginKey, type EditorState } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import { fromBase64, toBase64 } from "@atproto/lex-data";
import * as Y from "yjs";
import {
  absolutePositionToRelativePosition,
  relativePositionToAbsolutePosition,
  ySyncPluginKey,
} from "@tiptap/y-tiptap";

// getBinding reaches into the Collaboration extension's own ProseMirror
// plugin state for the live Yjs binding (its Y.XmlFragment + the
// PM-node/Y-type mapping) that absolutePositionToRelativePosition and
// relativePositionToAbsolutePosition both need. Undefined before
// Collaboration has finished its initial sync.
function getBinding(state: EditorState) {
  return ySyncPluginKey.getState(state)?.binding;
}

// encodeAnchor converts a ProseMirror selection range into the pair of
// base64 Yjs relative positions a network.habitat.docs.comment record's
// anchorStart/anchorEnd fields store (as the lexicon "bytes" type —
// https://atproto.com/specs/lexicon#bytes — see comments.server.ts's
// wrapping of these into {$bytes} for the actual record) — CRDT positions
// that survive concurrent edits made anywhere else in the document, unlike
// a plain character offset. toBase64 is @atproto/lex-data's own codec, the
// same one the record's eventual {$bytes} wrapper decodes with server-side
// (indigo's atdata.Bytes), so there's no risk of a mismatched base64
// variant between this and the server. Returns undefined if Collaboration
// hasn't synced yet.
export function encodeAnchor(
  state: EditorState,
  from: number,
  to: number,
): { anchorStart: string; anchorEnd: string } | undefined {
  const binding = getBinding(state);
  if (!binding) return undefined;
  const start = absolutePositionToRelativePosition(
    from,
    binding.type,
    binding.mapping,
  );
  const end = absolutePositionToRelativePosition(
    to,
    binding.type,
    binding.mapping,
  );
  return {
    anchorStart: toBase64(Y.encodeRelativePosition(start)),
    anchorEnd: toBase64(Y.encodeRelativePosition(end)),
  };
}

// decodeAnchor resolves a comment's stored anchor back into a current
// ProseMirror range — the whole point of storing CRDT relative positions
// rather than raw offsets: this works even after edits elsewhere in the
// document have shifted everything around. Returns undefined if the
// anchor no longer resolves to a valid range (e.g. the commented text was
// deleted entirely) or Collaboration hasn't synced yet — callers fall back
// to the comment's quotedText for display in that case.
export function decodeAnchor(
  state: EditorState,
  anchorStart: string,
  anchorEnd: string,
): { from: number; to: number } | undefined {
  const binding = getBinding(state);
  if (!binding) return undefined;
  const ydoc = binding.type.doc as Y.Doc | null;
  if (!ydoc) return undefined;
  try {
    const startRel = Y.decodeRelativePosition(fromBase64(anchorStart));
    const endRel = Y.decodeRelativePosition(fromBase64(anchorEnd));
    const from = relativePositionToAbsolutePosition(
      ydoc,
      binding.type,
      startRel,
      binding.mapping,
    );
    const to = relativePositionToAbsolutePosition(
      ydoc,
      binding.type,
      endRel,
      binding.mapping,
    );
    if (from === null || to === null || from > to) return undefined;
    return { from, to };
  } catch {
    // Malformed base64/relative-position bytes — treat like "doesn't
    // resolve" rather than crashing the editor over one bad comment.
    return undefined;
  }
}

export interface CommentAnchor {
  uri: string;
  anchorStart: string;
  anchorEnd: string;
}

const commentHighlightPluginKey = new PluginKey<DecorationSet>(
  "commentHighlight",
);

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    commentHighlight: {
      // setCommentHighlights replaces the set of comment ranges rendered
      // as highlights — called whenever the doc's comment list changes
      // (see $uri.tsx), not on every keystroke; the plugin itself
      // re-decodes anchors on every transaction regardless (cheap at
      // comment-thread scale) so highlights stay correctly positioned as
      // the document is edited.
      setCommentHighlights: (anchors: CommentAnchor[]) => ReturnType;
    };
  }
  interface Storage {
    commentHighlight: { anchors: CommentAnchor[] };
  }
}

// CommentHighlight renders each open comment thread's anchor as an inline
// decoration — not a persisted mark in the shared Yjs doc. Deliberately
// so: a comment's position is fully described by its own record's
// anchorStart/anchorEnd (see the comment lexicon), so no separate mutation
// of the document is needed just to show where a comment is, and comments
// don't show up as document edits in the doc's own edit history.
export const CommentHighlight = Extension.create({
  name: "commentHighlight",

  addStorage() {
    return { anchors: [] as CommentAnchor[] };
  },

  addCommands() {
    return {
      setCommentHighlights:
        (anchors: CommentAnchor[]) =>
        ({ editor, tr, dispatch }) => {
          editor.storage.commentHighlight.anchors = anchors;
          if (dispatch) {
            dispatch(tr.setMeta(commentHighlightPluginKey, true));
          }
          return true;
        },
    };
  },

  addProseMirrorPlugins() {
    const extensionStorage = this.storage;
    return [
      new Plugin<DecorationSet>({
        key: commentHighlightPluginKey,
        state: {
          init: () => DecorationSet.empty,
          apply(_tr, _old, _oldState, newState) {
            const decorations = extensionStorage.anchors
              .map((anchor: CommentAnchor) => {
                const range = decodeAnchor(
                  newState,
                  anchor.anchorStart,
                  anchor.anchorEnd,
                );
                if (!range) return undefined;
                return Decoration.inline(range.from, range.to, {
                  class: "comment-mark",
                  "data-comment-uri": anchor.uri,
                });
              })
              .filter((d: Decoration | undefined) => d !== undefined);
            return DecorationSet.create(newState.doc, decorations);
          },
        },
        props: {
          decorations(state) {
            return commentHighlightPluginKey.getState(state);
          },
        },
      }),
    ];
  },
});
