import { createFileRoute } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { AuthManager, Button } from "internal";
import {
  Card,
  CardContent,
  Separator,
  Spinner,
  Textarea,
} from "internal/components/ui";
import {
  createPost,
  createReply,
  postsQueryOptions,
  type PostView,
  type Thread,
} from "@/queries/posts";

export const Route = createFileRoute("/_requireAuth/")({
  component: Feed,
});

// OptimisticPost is a locally-created post shown immediately, before sap has
// ingested it back through the greensky server. isRoot distinguishes a new
// thread from a reply.
type OptimisticPost = PostView & { isRoot: boolean };

function Feed() {
  const { authManager } = Route.useRouteContext();
  const did = authManager.getAuthInfo()!.did;
  const { data: serverThreads } = useQuery(postsQueryOptions(authManager));
  const [optimistic, setOptimistic] = useState<OptimisticPost[]>([]);
  const [draft, setDraft] = useState("");

  // Once the server returns a post we created locally, drop the optimistic copy.
  useEffect(() => {
    if (!serverThreads) return;
    const known = new Set<string>();
    for (const t of serverThreads) {
      known.add(t.post.uri);
      for (const r of t.replies) known.add(r.uri);
    }
    setOptimistic((prev) => prev.filter((p) => !known.has(p.uri)));
  }, [serverThreads]);

  const postMutation = useMutation({
    mutationFn: (text: string) => createPost(authManager, text),
    onSuccess: ({ uri, spaceUri, createdAt }, text) => {
      setOptimistic((prev) => [
        { uri, spaceUri, author: did, text, createdAt, isRoot: true },
        ...prev,
      ]);
      setDraft("");
    },
  });

  const threads = mergeThreads(serverThreads ?? [], optimistic);

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-4 p-4">
      <Card>
        <CardContent className="flex flex-col gap-2 pt-6">
          <Textarea
            placeholder="What's on your mind?"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            rows={3}
          />
          <div className="flex justify-end">
            <Button
              onClick={() => postMutation.mutate(draft.trim())}
              disabled={!draft.trim() || postMutation.isPending}
            >
              {postMutation.isPending ? <Spinner /> : "Post"}
            </Button>
          </div>
        </CardContent>
      </Card>

      {!serverThreads && (
        <div className="flex justify-center py-8">
          <Spinner />
        </div>
      )}
      {serverThreads && threads.length === 0 && (
        <p className="py-8 text-center text-muted-foreground">
          No posts yet. Say something!
        </p>
      )}
      {threads.map((t) => (
        <ThreadCard
          key={t.post.uri}
          thread={t}
          authManager={authManager}
          authorDid={did}
          onReplied={(reply) => setOptimistic((prev) => [...prev, reply])}
        />
      ))}
    </div>
  );
}

function ThreadCard({
  thread,
  authManager,
  authorDid,
  onReplied,
}: {
  thread: Thread;
  authManager: AuthManager;
  authorDid: string;
  onReplied: (reply: OptimisticPost) => void;
}) {
  const [replyText, setReplyText] = useState("");
  const replyMutation = useMutation({
    mutationFn: (text: string) =>
      createReply(authManager, {
        spaceUri: thread.post.spaceUri,
        rootUri: thread.post.uri,
        parentUri: thread.post.uri,
        text,
      }),
    onSuccess: ({ uri, createdAt }, text) => {
      onReplied({
        uri,
        spaceUri: thread.post.spaceUri,
        author: authorDid,
        text,
        createdAt,
        isRoot: false,
      });
      setReplyText("");
    },
  });

  return (
    <Card>
      <CardContent className="flex flex-col gap-3 pt-6">
        <PostRow post={thread.post} />
        {thread.replies.length > 0 && (
          <>
            <Separator />
            <div className="flex flex-col gap-3 border-l pl-4">
              {thread.replies.map((r) => (
                <PostRow key={r.uri} post={r} />
              ))}
            </div>
          </>
        )}
        <div className="flex items-end gap-2">
          <Textarea
            placeholder="Reply…"
            value={replyText}
            onChange={(e) => setReplyText(e.target.value)}
            rows={1}
            className="min-h-9"
          />
          <Button
            variant="outline"
            onClick={() => replyMutation.mutate(replyText.trim())}
            disabled={!replyText.trim() || replyMutation.isPending}
          >
            Reply
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

function PostRow({ post }: { post: PostView }) {
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span className="font-medium text-foreground">
          {shortDid(post.author)}
        </span>
        <span>{formatTime(post.createdAt)}</span>
      </div>
      <p className="whitespace-pre-wrap break-words">{post.text}</p>
    </div>
  );
}

// mergeThreads overlays locally-created posts on top of the server's threads:
// optimistic root posts become new threads (prepended, newest first), and
// optimistic replies attach to their thread by shared space.
function mergeThreads(
  server: Thread[],
  optimistic: OptimisticPost[],
): Thread[] {
  const threads: Thread[] = server.map((t) => ({
    post: t.post,
    replies: [...t.replies],
  }));
  const bySpace = new Map<string, Thread>();
  for (const t of threads) bySpace.set(t.post.spaceUri, t);

  for (const p of optimistic) {
    if (!p.isRoot || bySpace.has(p.spaceUri)) continue;
    const thread: Thread = { post: stripOptimistic(p), replies: [] };
    bySpace.set(p.spaceUri, thread);
    threads.unshift(thread);
  }
  for (const p of optimistic) {
    if (p.isRoot) continue;
    const thread = bySpace.get(p.spaceUri);
    if (!thread || thread.post.uri === p.uri) continue;
    if (thread.replies.some((r) => r.uri === p.uri)) continue;
    thread.replies.push(stripOptimistic(p));
  }
  return threads;
}

function stripOptimistic(p: OptimisticPost): PostView {
  const { isRoot: _isRoot, ...view } = p;
  return view;
}

function shortDid(did: string): string {
  if (did.length <= 28) return did;
  return `${did.slice(0, 18)}…${did.slice(-6)}`;
}

function formatTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString();
}
