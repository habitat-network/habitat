import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { UserAvatar } from "internal";
import { Button, Textarea, toast } from "internal/components/ui";
import { useActors } from "@/hooks/useActors";
import {
  createComment,
  createReply,
  deleteCommentFn,
  deleteReplyFn,
  listComments,
  resolveComment,
  type StrongRef,
} from "@/server/functions";

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

  const authorDids = useMemo(
    () =>
      comments.flatMap((c) => [
        c.authorDid,
        ...c.replies.map((r) => r.authorDid),
      ]),
    [comments],
  );
  const getActor = useActors(authorDids);

  const [reply, setReply] = useState("");

  const invalidate = () => queryClient.invalidateQueries({ queryKey });

  function onMutationError(title: string) {
    return (err: Error) => {
      toast.add({ type: "error", title, description: err.message });
    };
  }

  // createCommentMutation writes the pending selection's root comment
  // record — the thread doesn't exist upstream until this succeeds.
  const createCommentMutation = useMutation({
    mutationFn: (anchor: PendingAnchor) =>
      createComment({
        data: {
          docId,
          body: reply,
          anchorStart: anchor.anchorStart,
          anchorEnd: anchor.anchorEnd,
          quotedText: anchor.quotedText,
        },
      }),
    onSuccess: () => {
      setReply("");
      onPendingAnchorResolved?.();
      invalidate();
    },
    onError: onMutationError("Couldn't post comment"),
  });

  const createReplyMutation = useMutation({
    mutationFn: (comment: StrongRef) =>
      createReply({ data: { docId, comment, body: reply } }),
    onSuccess: () => {
      setReply("");
      invalidate();
    },
    onError: onMutationError("Couldn't post reply"),
  });

  const resolveMutation = useMutation({
    mutationFn: (vars: { comment: StrongRef; resolved: boolean }) =>
      resolveComment({ data: { docId, ...vars } }),
    onSuccess: invalidate,
    onError: onMutationError("Couldn't update thread"),
  });

  const deleteCommentMutation = useMutation({
    mutationFn: (uri: string) => deleteCommentFn({ data: { docId, uri } }),
    onSuccess: invalidate,
    onError: onMutationError("Couldn't delete comment"),
  });

  const deleteReplyMutation = useMutation({
    mutationFn: (uri: string) => deleteReplyFn({ data: { docId, uri } }),
    onSuccess: invalidate,
    onError: onMutationError("Couldn't delete reply"),
  });

  const showPendingThread = pendingAnchor != null;

  if (comments.length === 0 && !showPendingThread) {
    return (
      <aside className="w-80 shrink-0 border-l p-4 text-sm text-muted-foreground">
        No comments yet. Select some text and click "Comment" to start a thread.
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
                disabled={createCommentMutation.isPending || !reply.trim()}
                onClick={() =>
                  pendingAnchor && createCommentMutation.mutate(pendingAnchor)
                }
              >
                Comment
              </Button>
            </div>
          </div>
        )}
        {comments.map((comment) => {
          const isActive = activeCommentUri === comment.uri;
          return (
            <div
              key={comment.uri}
              id={`thread-${comment.uri}`}
              className={"p-3 space-y-2 " + (isActive ? "bg-muted/50" : "")}
            >
              {comment.resolved && (
                <div className="text-xs text-muted-foreground">Resolved</div>
              )}
              {comment.quotedText && (
                <blockquote className="text-xs text-muted-foreground border-l-2 pl-2 italic">
                  &ldquo;{comment.quotedText}&rdquo;
                </blockquote>
              )}
              <div className="flex gap-2 group">
                <UserAvatar actor={getActor(comment.authorDid)} size="sm" />
                <div className="flex-1 min-w-0">
                  <div className="text-xs font-medium">
                    {getActor(comment.authorDid).displayName ||
                      getActor(comment.authorDid).handle ||
                      comment.authorDid}
                  </div>
                  <p className="text-sm whitespace-pre-wrap break-words">
                    {comment.body}
                  </p>
                </div>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  className="opacity-0 group-hover:opacity-100"
                  onClick={() => deleteCommentMutation.mutate(comment.uri)}
                >
                  ×
                </Button>
              </div>
              {comment.replies.map((r) => (
                <div key={r.uri} className="flex gap-2 group pl-4">
                  <UserAvatar actor={getActor(r.authorDid)} size="sm" />
                  <div className="flex-1 min-w-0">
                    <div className="text-xs font-medium">
                      {getActor(r.authorDid).displayName ||
                        getActor(r.authorDid).handle ||
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
                    onClick={() => deleteReplyMutation.mutate(r.uri)}
                  >
                    ×
                  </Button>
                </div>
              ))}
              <div className="flex items-center gap-2 pt-1">
                <Button
                  variant="ghost"
                  size="xs"
                  onClick={() =>
                    resolveMutation.mutate({
                      comment: { uri: comment.uri, cid: comment.cid },
                      resolved: !comment.resolved,
                    })
                  }
                >
                  {comment.resolved ? "Reopen" : "Resolve"}
                </Button>
              </div>
              {isActive && (
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
                    disabled={createReplyMutation.isPending || !reply.trim()}
                    onClick={() =>
                      createReplyMutation.mutate({
                        uri: comment.uri,
                        cid: comment.cid,
                      })
                    }
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
