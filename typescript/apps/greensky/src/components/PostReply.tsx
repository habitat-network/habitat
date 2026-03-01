import { useMutation } from "@tanstack/react-query";
import { AuthManager } from "internal";
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
      <button
        onClick={() => setOpen(true)}
        style={{
          position: "absolute",
          bottom: 8,
          right: 8,
          fontSize: "0.75em",
          padding: "2px 8px",
          cursor: "pointer",
        }}
      >
        ↩ Reply
      </button>
      <dialog open={open}>
        <article>
          <header>
            <button onClick={closeModal} aria-label="Close" rel="prev" />
            <p>
              <strong>Reply</strong>
            </p>
          </header>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              setReplyError(null);
              submitReply(undefined, {
                onError: (error) => setReplyError(error.message),
                onSuccess: () => closeModal(),
              });
            }}
          >
            <textarea
              placeholder="Write a reply..."
              value={text}
              onChange={(e) => setText(e.target.value)}
            />
            {replyError && <p>{replyError}</p>}
            <button type="submit" aria-busy={isPending}>
              Reply
            </button>
          </form>
        </article>
      </dialog>
    </>
  );
}
