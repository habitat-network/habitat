import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { Editor } from "@tiptap/react";
import { UserAvatar, getProfiles, type Actor } from "internal";
import { Button, Textarea, toast } from "internal/components/ui";
import {
  createComment,
  deleteCommentFn,
  listComments,
  resolveComment,
  type CommentView,
} from "@/server/functions";

// A thread is every comment sharing a threadId, oldest first — the mark's
// own quotedText/anchor position lives in the doc (via the CommentMark
// extension), not here; this just groups the records that discuss it.
interface Thread {
  threadId: string;
  comments: CommentView[];
  resolved: boolean;
}

function groupThreads(comments: CommentView[]): Thread[] {
  const byThread = new Map<string, CommentView[]>();
  for (const c of comments) {
    const list = byThread.get(c.threadId) ?? [];
    list.push(c);
    byThread.set(c.threadId, list);
  }
  return Array.from(byThread.entries()).map(([threadId, list]) => ({
    threadId,
    comments: list,
    // resolved is stamped on every row of a thread (see
    // setThreadResolved), so any one of them reflects the thread's state.
    resolved: list.some((c) => c.resolved),
  }));
}

// A PendingThread is a comment mark the user just applied to a selection
// (see $uri.tsx's startThread), before its first comment record exists —
// the sidebar renders it like any other thread (with 0 comments) so the
// compose box appears in the same place a reply box would.
export interface PendingThread {
  threadId: string;
  quotedText: string;
}

export interface CommentSidebarProps {
  docId: string;
  editor: Editor | null;
  // The thread a click on the doc's text just anchored to, if any — the
  // sidebar highlights it and expands its reply box. Cleared by the caller
  // once handled (see $uri.tsx's onThreadSelect).
  activeThreadId: string | null;
  pendingThread?: PendingThread | null;
  onPendingThreadResolved?: () => void;
  onClose: () => void;
}

export function CommentSidebar({
  docId,
  editor,
  activeThreadId,
  pendingThread,
  onPendingThreadResolved,
  onClose,
}: CommentSidebarProps) {
  const queryClient = useQueryClient();
  const queryKey = ["comments", docId];

  const { data: comments = [] } = useQuery({
    queryKey,
    queryFn: () => listComments({ data: { docId } }),
  });

  const authorDids = useMemo(
    () => Array.from(new Set(comments.map((c) => c.authorDid))),
    [comments],
  );
  const { data: profiles = [] } = useQuery({
    queryKey: ["profiles", authorDids],
    queryFn: () => getProfiles(authorDids),
    enabled: authorDids.length > 0,
  });
  const profileByDid = useMemo(
    () => new Map(profiles.map((p) => [p.did, p] as const)),
    [profiles],
  );

  const threads = useMemo(() => {
    const grouped = groupThreads(comments);
    // A pending thread (mark just applied, no comment record yet) has no
    // rows to group from — splice in an empty placeholder so it renders
    // (with its compose box) exactly where a real thread would.
    if (
      pendingThread &&
      !grouped.some((t) => t.threadId === pendingThread.threadId)
    ) {
      grouped.unshift({
        threadId: pendingThread.threadId,
        comments: [],
        resolved: false,
      });
    }
    return grouped;
  }, [comments, pendingThread]);
  const [reply, setReply] = useState("");
  const [posting, setPosting] = useState(false);

  const invalidate = () => queryClient.invalidateQueries({ queryKey });

  async function postReply(threadId: string) {
    if (!reply.trim()) return;
    setPosting(true);
    try {
      await createComment({
        data: {
          docId,
          threadId,
          body: reply,
          quotedText:
            pendingThread?.threadId === threadId
              ? pendingThread.quotedText
              : undefined,
        },
      });
      setReply("");
      if (pendingThread?.threadId === threadId) onPendingThreadResolved?.();
      await invalidate();
    } catch (err) {
      toast.add({
        type: "error",
        title: "Couldn't post comment",
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setPosting(false);
    }
  }

  async function toggleResolved(thread: Thread) {
    try {
      await resolveComment({
        data: {
          docId,
          threadId: thread.threadId,
          resolved: !thread.resolved,
        },
      });
      await invalidate();
    } catch (err) {
      toast.add({
        type: "error",
        title: "Couldn't update thread",
        description: err instanceof Error ? err.message : String(err),
      });
    }
  }

  async function deleteOne(comment: CommentView) {
    try {
      await deleteCommentFn({ data: { docId, uri: comment.uri } });
      await invalidate();
      // A deleted first comment can leave an empty thread — also drop its
      // anchor from the doc so the highlight doesn't point at nothing.
      const stillHasComments = comments.some(
        (c) => c.threadId === comment.threadId && c.uri !== comment.uri,
      );
      if (!stillHasComments) {
        editor?.chain().focus().unsetComment(comment.threadId).run();
      }
    } catch (err) {
      toast.add({
        type: "error",
        title: "Couldn't delete comment",
        description: err instanceof Error ? err.message : String(err),
      });
    }
  }

  if (threads.length === 0) {
    return (
      <aside className="w-80 shrink-0 border-l p-4 text-sm text-muted-foreground">
        No comments yet. Select some text and click "Comment" to start a
        thread.
        <div className="mt-4">
          <Button variant="ghost" size="sm" onClick={onClose}>
            Close
          </Button>
        </div>
      </aside>
    );
  }

  return (
    <aside className="w-80 shrink-0 border-l overflow-y-auto flex flex-col">
      <div className="flex items-center justify-between p-3 border-b">
        <h2 className="text-sm font-semibold">Comments</h2>
        <Button variant="ghost" size="sm" onClick={onClose}>
          Close
        </Button>
      </div>
      <div className="flex-1 divide-y">
        {threads.map((thread) => (
          <div
            key={thread.threadId}
            id={`thread-${thread.threadId}`}
            className={
              "p-3 space-y-2 " +
              (activeThreadId === thread.threadId ? "bg-muted/50" : "")
            }
          >
            {thread.resolved && (
              <div className="text-xs text-muted-foreground">Resolved</div>
            )}
            {(thread.comments[0]?.quotedText ??
              (pendingThread?.threadId === thread.threadId
                ? pendingThread.quotedText
                : undefined)) && (
              <blockquote className="text-xs text-muted-foreground border-l-2 pl-2 italic">
                &ldquo;
                {thread.comments[0]?.quotedText ?? pendingThread?.quotedText}
                &rdquo;
              </blockquote>
            )}
            {thread.comments.map((comment) => {
              const profile = profileByDid.get(comment.authorDid);
              const actor: Actor = profile ?? {
                did: comment.authorDid,
                handle: undefined,
                displayName: undefined,
                avatar: undefined,
              };
              return (
                <div key={comment.uri} className="flex gap-2 group">
                  <UserAvatar actor={actor} size="sm" />
                  <div className="flex-1 min-w-0">
                    <div className="text-xs font-medium">
                      {actor.displayName || actor.handle || actor.did}
                    </div>
                    <p className="text-sm whitespace-pre-wrap break-words">
                      {comment.body}
                    </p>
                  </div>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    className="opacity-0 group-hover:opacity-100"
                    onClick={() => deleteOne(comment)}
                  >
                    ×
                  </Button>
                </div>
              );
            })}
            {thread.comments.length > 0 && (
              <div className="flex items-center gap-2 pt-1">
                <Button
                  variant="ghost"
                  size="xs"
                  onClick={() => toggleResolved(thread)}
                >
                  {thread.resolved ? "Reopen" : "Resolve"}
                </Button>
              </div>
            )}
            {(activeThreadId === thread.threadId ||
              pendingThread?.threadId === thread.threadId) && (
              <div className="space-y-2 pt-1">
                <Textarea
                  autoFocus
                  value={reply}
                  onChange={(e) => setReply(e.target.value)}
                  placeholder="Reply..."
                  className="text-sm min-h-12"
                />
                <Button
                  size="sm"
                  disabled={posting || !reply.trim()}
                  onClick={() => postReply(thread.threadId)}
                >
                  Reply
                </Button>
              </div>
            )}
          </div>
        ))}
      </div>
    </aside>
  );
}
