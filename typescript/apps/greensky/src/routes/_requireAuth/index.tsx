import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { AuthManager } from "internal/authManager.js";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { getPrivatePosts, getProfile, type Profile } from "../../habitatApi";
import { type FeedEntry, Feed } from "../../Feed";

interface FormData {
  content: string;
  private: boolean;
}

interface BskyAuthor {
  handle: string;
  displayName?: string;
  avatar?: string;
}

interface BskyPost {
  uri: string;
  author?: BskyAuthor;
  record: {
    text: string;
    createdAt?: string;
  };
}

interface BskyFeedItem {
  post: BskyPost;
  reply?: {
    parent: {
      uri: string;
      author?: BskyAuthor;
    };
  };
  reason?: {
    $type: string;
    by: BskyAuthor;
  };
}

export const Route = createFileRoute("/_requireAuth/")({
  async loader({ context }) {
    const bskyItems: BskyFeedItem[] = await getBskyFeed(context.authManager);
    const privatePosts = await getPrivatePosts(context.authManager);

    // Parse the DID from the first private post's AT URI (at://DID/collection/rkey)
    // and use it to fetch the author's profile for display.
    const did = privatePosts[0]?.uri.split("/")[2];
    const privateAuthor: Profile | undefined = did
      ? await getProfile(context.authManager, did)
      : undefined;

    const entries: FeedEntry[] = [
      ...privatePosts.map(({ uri, value }): FeedEntry => ({
        uri,
        text: value.text,
        createdAt: value.createdAt,
        kind: "private",
        author: privateAuthor,
        replyToHandle: value.reply !== undefined ? null : undefined,
      })),
      ...bskyItems.map(({ post, reply, reason }): FeedEntry => ({
        uri: post.uri,
        text: post.record.text,
        createdAt: post.record.createdAt,
        kind: "public",
        author: post.author,
        replyToHandle: reply !== undefined
          ? (reply.parent.author?.handle ?? null)
          : undefined,
        repostedByHandle:
          reason?.$type === "app.bsky.feed.defs#reasonRepost"
            ? reason.by.handle
            : undefined,
      })),
    ];

    return entries;
  },
  component() {
    const { authManager } = Route.useRouteContext();
    const entries = Route.useLoaderData();
    const [modalOpen, setModalOpen] = useState(false);
    const { handleSubmit, register } = useForm<FormData>();
    const { mutate: createPost, isPending: createPostIsPending } = useMutation({
      mutationFn: async (data: FormData) => {
        await authManager.fetch(
          "/xrpc/network.habitat.putRecord",
          "POST",
          JSON.stringify({
            collection: "app.bsky.feed.post",
            record: {
              text: data.content,
              createdAt: new Date().toISOString(),
            },
            repo: authManager.handle,
          }),
        );
      },
    });
    return (
      <>
        <nav>
          <ul>
            <li>
              <h2>Greensky</h2>
            </li>
          </ul>
          <ul>
            <li>
              <span>@{(authManager as any).handle}</span>
            </li>
            <li>
              <button onClick={() => setModalOpen(true)}>New Post</button>
            </li>
          </ul>
        </nav>
        <Feed entries={entries} />
        <dialog open={modalOpen}>
          <article>
            <h1>New post</h1>
            <form
              onSubmit={handleSubmit(async (data) => {
                createPost(data, {
                  onError: (error) => {
                    alert(error.message);
                  },
                  onSuccess: () => {
                    setModalOpen(false);
                  },
                });
              })}
            >
              <textarea
                placeholder="What's on your mind?"
                {...register("content")}
              />
              <label>
                <input
                  type="checkbox"
                  {...register("private")}
                  defaultChecked
                  disabled
                />
                Private
              </label>
              <button type="submit" aria-busy={createPostIsPending}>
                Post
              </button>
            </form>
          </article>
        </dialog>
      </>
    );
  },
});

async function getBskyFeed(authManager: AuthManager): Promise<BskyFeedItem[]> {
  const headers = new Headers();
  headers.append("at-proxy", "did:web:api.bsky.app#bsky_appview");
  const params = new URLSearchParams();
  params.append(
    "feed",
    "at://did:plc:z72i7hdynmk6r22z27h6tvur/app.bsky.feed.generator/whats-hot",
  );
  params.append("limit", "10");
  const feedResponse = await authManager.fetch(
    `/xrpc/app.bsky.feed.getFeed?${params.toString()}`,
    "GET",
    null,
    headers,
  );
  const feedData: { feed: BskyFeedItem[] } = await feedResponse.json();
  return feedData.feed;
}
