import { useMutation } from "@tanstack/react-query";
import { AuthManager } from "internal";
import {
  Button,
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  Textarea,
} from "internal/components/ui";
import { useState } from "react";

interface PostReplyProps {
  postUri: string;
  postCid: string;
  postClique: string | undefined;
  authManager: AuthManager;
}

export function PostReply({
  postUri,
  postCid,
  postClique,
  authManager,
}: PostReplyProps) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const [replyError, setReplyError] = useState<string | null>(null);

  const closeModal = () => {
    setOpen(false);
    setText("");
    setReplyError(null);
  };

  const { mutate: submitReply, isPending } = useMutation({
    mutationFn: async () => {
      const did = authManager.getAuthInfo()!.did;
      const record = {
        $type: "app.bsky.feed.post",
        text,
        createdAt: new Date().toISOString(),
        reply: {
          root: { uri: postUri, cid: postCid },
          parent: { uri: postUri, cid: postCid },
        },
      };
      const grantees = postClique
        ? [{ $type: "network.habitat.grantee#cliqueRef", uri: postClique }]
        : [];

      const res = await authManager.fetch(
        "/xrpc/network.habitat.putRecord",
        "POST",
        JSON.stringify({
          repo: did,
          collection: "app.bsky.feed.post",
          record,
          grantees,
        }),
      );

      if (!res.ok) {
        let message = `Request failed: ${res.status} ${res.statusText}`;
        try {
          const body = await res.json();
          if (body.message) message = body.message;
          else if (body.error) message = body.error;
        } catch {}
        throw new Error(message);
      }
    },
  });

  return (
    <>
      <Button
        onClick={() => setOpen(true)}
        variant="ghost"
        size="sm"
        className="absolute bottom-2 right-2"
      >
        ↩ Reply
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Reply</DialogTitle>
          </DialogHeader>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              setReplyError(null);
              submitReply(undefined, {
                onError: (error) => setReplyError(error.message),
                onSuccess: () => closeModal(),
              });
            }}
            className="space-y-4"
          >
            <Textarea
              placeholder="Write a reply..."
              value={text}
              onChange={(e) => setText(e.target.value)}
            />
            {replyError && <p className="text-destructive text-sm">{replyError}</p>}
            <Button type="submit" disabled={isPending}>
              {isPending ? "Replying..." : "Reply"}
            </Button>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}
