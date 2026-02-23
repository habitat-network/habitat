import { createFileRoute } from "@tanstack/react-router";
import { AuthManager } from "internal/authManager.js";

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

interface PrivatePostRecord {
  text: string;
  reply?: {
    parent: { uri: string };
    root: { uri: string };
  };
}

interface PrivatePost {
  uri: string;
  cid: string;
  value: PrivatePostRecord;
}

interface Profile {
  handle: string;
  displayName?: string;
  avatar?: string;
}

export const Route = createFileRoute("/_requireAuth/handle/$handle")({
  async loader({ context, params }): Promise<{
    publicItems: FeedItem[];
    privateItems: PrivatePost[];
    profile: Profile;
  }> {
    const publicItems = await getAuthorFeed(context.authManager, params.handle);
    const privateItems = await getPrivatePosts(context.authManager, params.handle);
    const profile = await getProfile(context.authManager, params.handle);
    return { publicItems, privateItems, profile };
  },
  component() {
    const { handle } = Route.useParams();
    const { publicItems, privateItems, profile } = Route.useLoaderData();

    type UnifiedItem =
      | { kind: "public"; item: FeedItem }
      | { kind: "private"; item: PrivatePost };

    const feed: UnifiedItem[] = [
      ...privateItems.map((item) => ({ kind: "private" as const, item })),
      ...publicItems.map((item) => ({ kind: "public" as const, item })),
    ];

    return (
      <>
        <nav>
          <ul>
            <li>
              <h2>@{handle}</h2>
            </li>
          </ul>
        </nav>
        {feed.map((entry) =>
          entry.kind === "private" ? (
            <article
              key={entry.item.uri}
              style={{ outline: "2px solid darkgreen" }}
            >
              <header>
                {entry.item.value.reply && (
                  <div style={{ fontSize: "0.75em", color: "gray", marginBottom: 4 }}>
                    ← reply
                  </div>
                )}
                <span>
                  <img
                    src={profile.avatar}
                    width={24}
                    height={24}
                    style={{ marginRight: 8 }}
                  />
                  {profile.displayName ?? profile.handle}
                </span>
              </header>
              {entry.item.value.text}
            </article>
          ) : (
            <article
              key={entry.item.post.uri}
              style={{ outline: "2px solid blue" }}
            >
              <header>
                {entry.item.reason?.$type === "app.bsky.feed.defs#reasonRepost" && (
                  <div style={{ fontSize: "0.75em", color: "gray", marginBottom: 4 }}>
                    ↻ reposted by @{entry.item.reason.by.handle}
                  </div>
                )}
                {entry.item.reply && (
                  <div style={{ fontSize: "0.75em", color: "gray", marginBottom: 4 }}>
                    ← reply to @{entry.item.reply.parent.author?.handle}
                  </div>
                )}
                <span>
                  <img
                    src={entry.item.post.author?.avatar ?? profile.avatar}
                    width={24}
                    height={24}
                    style={{ marginRight: 8 }}
                  />
                  {entry.item.post.author?.displayName ?? profile.displayName ?? profile.handle}
                </span>
              </header>
              {entry.item.post.record.text}
            </article>
          ),
        )}
      </>
    );
  },
});

async function getProfile(
  authManager: AuthManager,
  handle: string,
): Promise<Profile> {
  const headers = new Headers();
  headers.append("at-proxy", "did:web:api.bsky.app#bsky_appview");
  const params = new URLSearchParams();
  params.append("actor", handle);
  const response = await authManager.fetch(
    `/xrpc/app.bsky.actor.getProfile?${params.toString()}`,
    "GET",
    null,
    headers,
  );
  return response.json();
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

async function getPrivatePosts(
  authManager: AuthManager,
  handle: string,
): Promise<PrivatePost[]> {
  const response = await authManager.fetch(
    "/xrpc/network.habitat.listRecords",
    "POST",
    JSON.stringify({
      subjects: [handle],
      collection: "network.habitat.post",
    }),
  );
  const data: { records?: PrivatePost[] } = await response.json();
  return data.records ?? [];
}
