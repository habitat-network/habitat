import { Mark, mergeAttributes } from "@tiptap/core";

// CommentMark tags a range of the document with a comment thread's id. It
// carries no comment content itself — that lives in the
// network.habitat.docs.comment records the comments space stores (see
// src/server/comments.server.ts) — this mark only anchors a thread to a
// span of text so the editor can highlight it and the sidebar can scroll
// to it. Being a Yjs-backed ProseMirror mark, it syncs over the same
// Collaboration extension/WebSocket as the rest of the document: applying
// or removing it is itself a doc edit, requiring writer on the doc (the
// same requirement Collaboration already enforces for any edit).
//
// threadId is stored, not the comment records themselves — those come from
// listComments (D1-backed, see functions.ts), keyed by threadId, so a range
// tagged here just needs to say which thread it belongs to.
export interface CommentMarkOptions {
  HTMLAttributes: Record<string, unknown>;
}

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    comment: {
      // setComment tags the current selection with threadId, creating a
      // new thread's anchor.
      setComment: (threadId: string) => ReturnType;
      // unsetComment removes a thread's anchor from the current selection
      // (or, more commonly, the caller re-selects the marked range first —
      // see CommentSidebar's "resolve" action, which just hides a resolved
      // thread rather than unmarking it: unmarking is for actually
      // deleting a thread's last comment).
      unsetComment: (threadId: string) => ReturnType;
    };
  }
}

export const CommentMark = Mark.create<CommentMarkOptions>({
  name: "comment",

  // Multiple threads can overlap the same text (a reply thread nested
  // inside another's range), and a single click should be able to
  // distinguish which thread was clicked — both need distinct mark
  // instances per thread rather than ProseMirror's default of coalescing
  // marks with the same attrs, so excludes/inclusive stay at their
  // permissive defaults and identity is carried entirely by threadId.
  addOptions() {
    return { HTMLAttributes: {} };
  },

  addAttributes() {
    return {
      threadId: {
        default: null,
        parseHTML: (el) => el.getAttribute("data-comment-thread"),
        renderHTML: (attrs) =>
          attrs.threadId ? { "data-comment-thread": attrs.threadId } : {},
      },
    };
  },

  parseHTML() {
    return [{ tag: "span[data-comment-thread]" }];
  },

  renderHTML({ HTMLAttributes }) {
    return [
      "span",
      mergeAttributes(this.options.HTMLAttributes, HTMLAttributes, {
        class: "comment-mark",
      }),
      0,
    ];
  },

  addCommands() {
    return {
      setComment:
        (threadId: string) =>
        ({ commands }) =>
          commands.setMark(this.name, { threadId }),
      unsetComment:
        (threadId: string) =>
        ({ tr, state, dispatch }) => {
          // Only remove the mark instance for this threadId, not every
          // comment mark on the selection — a range can carry more than
          // one thread's anchor (see the addAttributes comment above).
          const { from, to } = state.selection;
          const markType = state.schema.marks[this.name];
          tr.doc.nodesBetween(from, to, (node, pos) => {
            const mark = node.marks.find(
              (m) => m.type === markType && m.attrs.threadId === threadId,
            );
            if (mark) {
              tr.removeMark(
                Math.max(pos, from),
                Math.min(pos + node.nodeSize, to),
                mark,
              );
            }
          });
          if (dispatch) dispatch(tr);
          return true;
        },
    };
  },
});
