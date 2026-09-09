import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { UserAvatar, type Actor } from "internal";
import { Button, Textarea, toast } from "internal/components/ui";
import { useActors } from "@/hooks/useActors";
import {
  createComment,
  createReply,
  deleteCommentFn,
  deleteReplyFn,
  listComments,
  resolveComment,
  type CommentReplyView,
  type CommentView,
  type StrongRef,
} from "@/server/functions";

// A Thread groups a root network.habitat.docs.comment with its replies —
// the root carries the thread's CRDT anchor into the doc (see the comment
// lexicon), replies just reference it by strongRef. root is undefined for
// an "orphaned" group: replies whose root comment no longer exists in the
// current comment list (e.g. its author deleted it) — shown so a reply
// isn't simply invisible, since deleting the root is not otherwise
// coordinated with cleaning up its replies.
interface Thread {
  commentUri: string;
  root: CommentView | undefined;
  replies: CommentReplyView[];
  resolved: boolean;
}

function groupThreads(
  comments: CommentView[],
  replies: CommentReplyView[],
  resolvedCommentUris: readonly string[],
): Thread[] {
  const resolved = new Set(resolvedCommentUris);
  const byRoot = new Map<string, Thread>();
  for (const c of comments) {
    byRoot.set(c.uri, {
      commentUri: c.uri,
      root: c,
      replies: [],
      resolved: resolved.has(c.uri),
    });
  }
  for (const r of replies) {
    let thread = byRoot.get(r.commentUri);
    if (!thread) {
      thread = {
        commentUri: r.commentUri,
        root: undefined,
        replies: [],
        resolved: resolved.has(r.commentUri),
      };
      byRoot.set(r.commentUri, thread);
    }
    thread.replies.push(r);
  }
  return Array.from(byRoot.values());
}

// A PendingAnchor is a CRDT range the user just selected and clicked
// "Comment" on (see $uri.tsx's startThread), before its root comment
// record exists — the sidebar renders it like any other thread (with no
// root yet) so the compose box appears in the same place a reply box
// would. anchorStart/anchorEnd ride along only to be handed back to
// createComment on submit; the sidebar itself never decodes them.
export interface PendingAnchor {
  anchorStart: string;
  anchorEnd: string;
  quotedText: string;
}

export interface CommentSidebarProps {
  docId: string;
  // The thread (by its root comment's URI) a click on a doc highlight just
  // selected, if any — the sidebar highlights it and expands its reply
  // box. Cleared by the caller once handled (see $uri.tsx).
  activeCommentUri: string | null;
  pendingAnchor?: PendingAnchor | null;
  onPendingAnchorResolved?: () => void;
  onClose: () => void;
}

export function CommentSidebar({
  docId,
  activeCommentUri,
  pendingAnchor,
  onPendingAnchorResolved,
  onClose,
}: CommentSidebarProps) {
  const queryClient = useQueryClient();
  const queryKey = ["comments", docId];

  const { data } = useQuery({
    queryKey,
    queryFn: () => listComments({ data: { docId } }),
  });
  const comments = data?.comments ?? [];
  const replies = data?.replies ?? [];
  const resolvedCommentUris = data?.resolvedCommentUris ?? [];

  const authorDids = useMemo(
    () => [
      ...comments.map((c) => c.authorDid),
      ...replies.map((r) => r.authorDid),
    ],
    [comments, replies],
  );
  const profileByDid = useActors(authorDids);

  const threads = useMemo(
    () => groupThreads(comments, replies, resolvedCommentUris),
    [comments, replies, resolvedCommentUris],
  );
  const [reply, setReply] = useState("");
  const [posting, setPosting] = useState(false);

  const invalidate = () => queryClient.invalidateQueries({ queryKey });

  function actorFor(did: string): Actor {
    return (
      profileByDid.get(did) ?? {
        did,
        handle: undefined,
        displayName: undefined,
        avatar: undefined,
      }
    );
  }

  // startFirstComment writes the pending selection's root comment record —
  // the thread doesn't exist upstream until this succeeds.
  async function startFirstComment() {
    if (!pendingAnchor || !reply.trim()) return;
    setPosting(true);
    try {
      const created = await createComment({
        data: {
          docId,
          body: reply,
          anchorStart: pendingAnchor.anchorStart,
          anchorEnd: pendingAnchor.anchorEnd,
          quotedText: pendingAnchor.quotedText,
        },
      });
      setReply("");
      onPendingAnchorResolved?.();
      await invalidate();
      return created;
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

  async function postReply(root: CommentView) {
    if (!reply.trim()) return;
    setPosting(true);
    try {
      const comment: StrongRef = { uri: root.uri, cid: root.cid };
      await createReply({ data: { docId, comment, body: reply } });
      setReply("");
      await invalidate();
    } catch (err) {
      toast.add({
        type: "error",
        title: "Couldn't post reply",
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setPosting(false);
    }
  }

  async function toggleResolved(thread: Thread) {
    if (!thread.root) return; // nothing to resolve without a live root
    try {
      await resolveComment({
        data: {
          docId,
          comment: { uri: thread.root.uri, cid: thread.root.cid },
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

  async function deleteRoot(comment: CommentView) {
    try {
      await deleteCommentFn({ data: { docId, uri: comment.uri } });
      await invalidate();
    } catch (err) {
      toast.add({
        type: "error",
        title: "Couldn't delete comment",
        description: err instanceof Error ? err.message : String(err),
      });
    }
  }

  async function deleteReply(reply: CommentReplyView) {
    try {
      await deleteReplyFn({ data: { docId, uri: reply.uri } });
      await invalidate();
    } catch (err) {
      toast.add({
        type: "error",
        title: "Couldn't delete reply",
        description: err instanceof Error ? err.message : String(err),
      });
    }
  }

  const showPendingThread = pendingAnchor != null;

  if (threads.length === 0 && !showPendingThread) {
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
        {showPendingThread && (
          <div className="p-3 space-y-2 bg-muted/50">
            <blockquote className="text-xs text-muted-foreground border-l-2 pl-2 italic">
              &ldquo;{pendingAnchor.quotedText}&rdquo;
            </blockquote>
            <div className="space-y-2 pt-1">
              <Textarea
                autoFocus
                value={reply}
                onChange={(e) => setReply(e.target.value)}
                placeholder="Comment..."
                className="text-sm min-h-12"
              />
              <Button
                size="sm"
                disabled={posting || !reply.trim()}
                onClick={startFirstComment}
              >
                Comment
              </Button>
            </div>
          </div>
        )}
        {threads.map((thread) => {
          const isActive = activeCommentUri === thread.commentUri;
          return (
            <div
              key={thread.commentUri}
              id={`thread-${thread.commentUri}`}
              className={"p-3 space-y-2 " + (isActive ? "bg-muted/50" : "")}
            >
              {thread.resolved && (
                <div className="text-xs text-muted-foreground">Resolved</div>
              )}
              {!thread.root && (
                <div className="text-xs text-muted-foreground italic">
                  The comment this replied to was deleted.
                </div>
              )}
              {thread.root?.quotedText && (
                <blockquote className="text-xs text-muted-foreground border-l-2 pl-2 italic">
                  &ldquo;{thread.root.quotedText}&rdquo;
                </blockquote>
              )}
              {thread.root && (
                <div className="flex gap-2 group">
                  <UserAvatar actor={actorFor(thread.root.authorDid)} size="sm" />
                  <div className="flex-1 min-w-0">
                    <div className="text-xs font-medium">
                      {actorFor(thread.root.authorDid).displayName ||
                        actorFor(thread.root.authorDid).handle ||
                        thread.root.authorDid}
                    </div>
                    <p className="text-sm whitespace-pre-wrap break-words">
                      {thread.root.body}
                    </p>
                  </div>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    className="opacity-0 group-hover:opacity-100"
                    onClick={() => deleteRoot(thread.root!)}
                  >
                    ×
                  </Button>
                </div>
              )}
              {thread.replies.map((r) => (
                <div key={r.uri} className="flex gap-2 group pl-4">
                  <UserAvatar actor={actorFor(r.authorDid)} size="sm" />
                  <div className="flex-1 min-w-0">
                    <div className="text-xs font-medium">
                      {actorFor(r.authorDid).displayName ||
                        actorFor(r.authorDid).handle ||
                        r.authorDid}
                    </div>
                    <p className="text-sm whitespace-pre-wrap break-words">
                      {r.body}
                    </p>
                  </div>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    className="opacity-0 group-hover:opacity-100"
                    onClick={() => deleteReply(r)}
                  >
                    ×
                  </Button>
                </div>
              ))}
              {thread.root && (
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
              {isActive && thread.root && (
                <div className="space-y-2 pt-1">
                  <Textarea
                    autoFocus
                    value={reply}
                    onChange={(e) => setReply(e.target.value)}
                    placeholder="Reply..."
                    className="text-sm"
                  />
                  <Button
                    size="sm"
                    disabled={posting || !reply.trim()}
                    onClick={() => postReply(thread.root!)}
                  >
                    Reply
                  </Button>
                </div>
              )}
            </div>
          );
        })}
      </div>
    </aside>
  );
}
