import * as Y from "yjs";
import {
  absolutePositionToRelativePosition,
  relativePositionToAbsolutePosition,
  ySyncPluginKey,
  type ProsemirrorBinding,
} from "@tiptap/y-tiptap";
import type { Editor } from "@tiptap/react";

export interface CommentRange {
  start: string;
  end: string;
}

export function relPosToString(rel: Y.RelativePosition): string {
  return JSON.stringify(Y.relativePositionToJSON(rel));
}

export function stringToRelPos(s: string): Y.RelativePosition {
  return Y.createRelativePositionFromJSON(JSON.parse(s));
}

function ySync(editor: Editor): {
  type: Y.XmlFragment;
  doc: Y.Doc;
  binding: ProsemirrorBinding;
} | null {
  const state = ySyncPluginKey.getState(editor.state);
  if (!state || !state.binding) {
    return null;
  }
  return state as {
    type: Y.XmlFragment;
    doc: Y.Doc;
    binding: ProsemirrorBinding;
  };
}

export function rangeFromSelection(editor: Editor): CommentRange | null {
  const { from, to } = editor.state.selection;
  if (from === to) {
    return null;
  }
  const sync = ySync(editor);
  if (!sync) {
    return null;
  }
  const start = absolutePositionToRelativePosition(
    from,
    sync.type,
    sync.binding.mapping,
  );
  const end = absolutePositionToRelativePosition(
    to,
    sync.type,
    sync.binding.mapping,
  );
  return { start: relPosToString(start), end: relPosToString(end) };
}

export function resolveRange(
  editor: Editor,
  range: CommentRange,
): { from: number; to: number } | null {
  const sync = ySync(editor);
  if (!sync) {
    return null;
  }
  const from = relativePositionToAbsolutePosition(
    sync.doc,
    sync.type,
    stringToRelPos(range.start),
    sync.binding.mapping,
  );
  const to = relativePositionToAbsolutePosition(
    sync.doc,
    sync.type,
    stringToRelPos(range.end),
    sync.binding.mapping,
  );
  if (from == null || to == null) {
    return null;
  }
  return { from, to };
}
