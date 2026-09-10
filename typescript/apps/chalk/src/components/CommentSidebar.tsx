import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { UserAvatar } from "internal";
import { Button, Textarea, toast } from "internal/components/ui";
import { X } from "lucide-react";
import { useActors } from "@/hooks/useActors";
import {
  createComment,
  createReply,
  deleteCommentFn,
  deleteReplyFn,
  listComments,
  type StrongRef,
} from "@/server/functions";

// A PendingAnchor is a CRDT range the user just selected and clicked
// "Comment" on (see $uri.tsx's startThread), before its root comment
// record exists — the sidebar renders it like any other thread (with no
// root yet) so the compose box appears in the same place a reply box
// would. anchorStart/anchorEnd ride along only to be handed back to
// createComment on submit; the sidebar itself never decodes them.
export interface PendingAnchor {
  anchorStart: Uint8Array;
  anchorEnd: Uint8Array;
  quotedText: string;
}

export interface CommentSidebarProps {
  docId: string;
  // The viewer's own DID. Comment and reply records live in their author's
  // repo and only that author may delete one (see comments.server.ts's
  // removeComment/removeReply, and pear's own repo-scoped deleteRecord
  // underneath it), so this is what decides whether a delete affordance is
  // shown at all — without it the sidebar would offer everyone a button
  // that reliably fails.
  currentUserDid: string;
  // Whether the viewer may write comments at all — true for editors and
  // commenters, false for a viewer, who sees every thread but gets no
  // compose or reply box. It also gates the delete buttons: someone
  // demoted to viewer still authored their old comments, but no longer
  // holds writer on the comments space, so pear would reject the delete.
  // The server enforces the same rule (see canComment in functions.ts);
  // this only decides what is rendered.
  canComment: boolean;
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
  currentUserDid,
  canComment,
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
  // Memoized on `data`, not on a freshly-built `?? []`: a new array
  // identity every render would make the useMemo below (and useActors'
  // query key through it) churn on every render.
  const comments = useMemo(() => data?.comments ?? [], [data]);

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

  // A viewer can't start a thread, so a pending selection can't reach the
  // sidebar for them — but guard here too rather than trusting the caller
  // to never pass one.
  const showPendingThread = pendingAnchor != null && canComment;

  if (comments.length === 0 && !showPendingThread) {
    return (
      <aside className="w-80 shrink-0 border-l p-4 text-sm text-muted-foreground">
        {canComment
          ? 'No comments yet. Select some text and click "Comment" to start a thread.'
          : "No comments yet."}
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
                {canComment && comment.authorDid === currentUserDid && (
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    className="opacity-0 group-hover:opacity-100"
                    aria-label="Delete comment"
                    title="Delete comment"
                    onClick={() => deleteCommentMutation.mutate(comment.uri)}
                  >
                    <X />
                  </Button>
                )}
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
                  {canComment && r.authorDid === currentUserDid && (
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      className="opacity-0 group-hover:opacity-100"
                      aria-label="Delete reply"
                      title="Delete reply"
                      onClick={() => deleteReplyMutation.mutate(r.uri)}
                    >
                      <X />
                    </Button>
                  )}
                </div>
              ))}
              {isActive && canComment && (
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
