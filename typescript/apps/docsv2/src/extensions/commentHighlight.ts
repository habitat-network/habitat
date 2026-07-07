import { Extension } from "@tiptap/react";
import type { Editor } from "@tiptap/react";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import { resolveRange, type CommentRange } from "@/lib/anchor";

export interface HighlightRange {
  id: string;
  range: CommentRange;
}

const highlightKey = new PluginKey<HighlightRange[]>("comment-highlights");

export const CommentHighlight = Extension.create({
  name: "commentHighlight",
  addProseMirrorPlugins() {
    const editor = this.editor;
    return [
      new Plugin<HighlightRange[]>({
        key: highlightKey,
        state: {
          init: () => [],
          apply(tr, value) {
            const next = tr.getMeta(highlightKey) as
              | HighlightRange[]
              | undefined;
            return next ?? value;
          },
        },
        props: {
          decorations(state) {
            const ranges = highlightKey.getState(state) ?? [];
            const decos: Decoration[] = [];
            for (const { id, range } of ranges) {
              const resolved = resolveRange(editor, range);
              if (!resolved || resolved.from >= resolved.to) {
                continue;
              }
              decos.push(
                Decoration.inline(resolved.from, resolved.to, {
                  class: "comment-highlight",
                  "data-comment-id": id,
                }),
              );
            }
            return DecorationSet.create(state.doc, decos);
          },
        },
      }),
    ];
  },
});

export function setCommentHighlights(
  editor: Editor,
  ranges: HighlightRange[],
): void {
  editor.view.dispatch(editor.state.tr.setMeta(highlightKey, ranges));
}
