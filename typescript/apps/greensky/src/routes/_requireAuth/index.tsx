import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { AuthManager } from "internal/authManager.js";
import { useState } from "react";
import { useForm } from "react-hook-form";

interface FormData {
  content: string;
  private: boolean;
}

export const Route = createFileRoute("/_requireAuth/")({
  async loader({ context }) {
    const bskyFeed = await getBskyFeed(context.authManager);
    const habitatFeed = await getHabitatFeed(context.authManager);
    return [...habitatFeed, ...bskyFeed.feed];
  },
  component() {
    const { authManager } = Route.useRouteContext();
    const data = Route.useLoaderData();
    const [modalOpen, setModalOpen] = useState(false);
    const { handleSubmit, register } = useForm<FormData>();
    const { mutate: createPost, isPending: createPostIsPending } = useMutation({
      mutationFn: async (data: FormData) => {
        await authManager.fetch(
          "/xrpc/network.habitat.putRecord",
          "POST",
          JSON.stringify({
            collection: "network.habitat.post",
            record: {
              text: data.content,
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
              <button onClick={() => setModalOpen(true)}>New Post</button>
            </li>
          </ul>
        </nav>
        {data.map(({ post }) => {
          return (
            <article key={post.uri}>
              <header>
                <span>
                  <img
                    src={post.author.avatar}
                    width={24}
                    height={24}
                    style={{ marginRight: 8 }}
                  />
                  {post.author.displayName}
                </span>
              </header>
              {post.record.text}
            </article>
          );
        })}
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

interface Post {
  uri: string;
  author: {
    displayName: string;
    avatar?: string;
  };
  record: {
    text: string;
  };
}

async function getBskyFeed(
  authManager: AuthManager,
): Promise<{ feed: { post: Post }[] }> {
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
  return feedResponse.json();
}

async function getHabitatFeed(authManager: AuthManager) {
  const params = new URLSearchParams();
  params.set("limit", "10");
  const response = await authManager.fetch(
    `/xrpc/network.habitat.notification.listNotifications?${params.toString()}`,
    "GET",
  );

  const notifications: {
    records: {
      uri: string;
      cid: string;
      value: {
        originDid: string;
        collection: string;
        rkey: string;
      };
    }[];
    cursor?: string;
  } = await response.json();

  const postRequests = notifications.records
    .filter((record) => {
      return record.value.collection === "network.habitat.post";
    })
    .map(async (notification): Promise<{ post: Post }> => {
      const params = new URLSearchParams();
      params.set("collection", notification.value.collection);
      params.set("rkey", notification.value.rkey);
      params.set("repo", notification.value.originDid);
      const response = await authManager.fetch(
        "/xrpc/network.habitat.getRecord",
      );
      const post: { uri: string; value: { text: string } } =
        await response.json();
      return {
        post: {
          uri: post.uri,
          record: { text: post.value.text },
          author: {
            displayName: notification.value.originDid,
          },
        },
      };
    });

  const posts = await Promise.all(postRequests);
  return posts;
}
