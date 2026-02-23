import { createFileRoute } from "@tanstack/react-router";
import { AuthManager } from "internal/authManager.js";

export const Route = createFileRoute("/_requireAuth/handle/$handle")({
  async loader({ context, params }) {
    return getAuthorFeed(context.authManager, params.handle);
  },
  component() {
    const { handle } = Route.useParams();
    const items = Route.useLoaderData();
    return (
      <>
        <nav>
          <ul>
            <li>
              <h2>@{handle}</h2>
            </li>
          </ul>
        </nav>
        {items.map((item) => (
          <article key={item.post.uri}>
            <header>
              {item.reason?.$type === "app.bsky.feed.defs#reasonRepost" && (
                <div style={{ fontSize: "0.75em", color: "gray", marginBottom: 4 }}>
                  ↻ reposted by @{item.reason.by.handle}
                </div>
              )}
              {item.reply && (
                <div style={{ fontSize: "0.75em", color: "gray", marginBottom: 4 }}>
                  ← reply to @{item.reply.parent.author?.handle}
                </div>
              )}
              <span>
                <img
                  src={item.post.author?.avatar}
                  width={24}
                  height={24}
                  style={{ marginRight: 8 }}
                />
                {item.post.author?.displayName}
              </span>
            </header>
            {item.post.record.text}
          </article>
        ))}
      </>
    );
  },
});

interface Author {
  handle: string;
  displayName: string;
  avatar?: string;
}

interface Post {
  uri: string;
  author?: Author;
  record: {
    text: string;
  };
}

interface FeedItem {
  post: Post;
  reply?: {
    parent: {
      uri: string;
      author?: Author;
    };
  };
  reason?: {
    $type: string;
    by: Author;
  };
}

async function getAuthorFeed(
  authManager: AuthManager,
  handle: string,
): Promise<FeedItem[]> {
  const headers = new Headers();
  headers.append("at-proxy", "did:web:api.bsky.app#bsky_appview");
  const params = new URLSearchParams();
  params.append("actor", handle);
  params.append("limit", "30");
  const response = await authManager.fetch(
    `/xrpc/app.bsky.feed.getAuthorFeed?${params.toString()}`,
    "GET",
    null,
    headers,
  );
  const data: { feed: FeedItem[] } = await response.json();
  return data.feed;
}
