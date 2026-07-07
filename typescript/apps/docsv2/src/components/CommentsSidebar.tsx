import { useState } from "react";
import { Button } from "internal/components/ui";
import type { CommentThread } from "@/queries/comments";

interface CommentsSidebarProps {
  threads: CommentThread[];
  canComment: boolean;
  onSelect: (thread: CommentThread) => void;
  onReply: (parentUri: string, body: string) => void;
  isReplying: string | null;
}

function shortDid(did: string): string {
  return did.replace(/^did:web:/, "");
}

export function CommentsSidebar({
  threads,
  canComment,
  onSelect,
  onReply,
  isReplying,
}: CommentsSidebarProps) {
  return (
    <aside className="w-80 shrink-0 border-l overflow-y-auto p-3 flex flex-col gap-3">
      <h2 className="text-sm font-medium text-muted-foreground">Comments</h2>
      {threads.length === 0 && (
        <p className="text-sm text-muted-foreground">No comments yet.</p>
      )}
      {threads.map((thread) => (
        <div key={thread.uri} className="rounded border p-2 flex flex-col gap-2">
          <button
            type="button"
            className="text-left"
            onClick={() => onSelect(thread)}
          >
            <div className="text-xs text-muted-foreground">
              {shortDid(thread.author)}
            </div>
            <div className="text-sm">{thread.body}</div>
          </button>
          {thread.replies.map((reply) => (
            <div key={reply.uri} className="ml-3 border-l pl-2">
              <div className="text-xs text-muted-foreground">
                {shortDid(reply.author)}
              </div>
              <div className="text-sm">{reply.body}</div>
            </div>
          ))}
          {canComment && (
            <ReplyBox
              disabled={isReplying === thread.uri}
              onSubmit={(body) => onReply(thread.uri, body)}
            />
          )}
        </div>
      ))}
    </aside>
  );
}

function ReplyBox({
  onSubmit,
  disabled,
}: {
  onSubmit: (body: string) => void;
  disabled: boolean;
}) {
  const [value, setValue] = useState("");
  return (
    <form
      className="flex gap-1"
      onSubmit={(e) => {
        e.preventDefault();
        const body = value.trim();
        if (!body) return;
        onSubmit(body);
        setValue("");
      }}
    >
      <input
        className="flex-1 rounded border px-2 py-1 text-sm outline-none"
        placeholder="Reply\u2026"
        value={value}
        onChange={(e) => setValue(e.target.value)}
      />
      <Button type="submit" size="sm" disabled={disabled}>
        Reply
      </Button>
    </form>
  );
}
