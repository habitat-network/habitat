import { createFileRoute, Link } from "@tanstack/react-router";
import { AuthManager } from "internal/authManager.js";
import { type PrivatePost, type Profile, getPrivatePosts, getProfile } from "../../habitatApi";
import { type FeedEntry, Feed } from "../../Feed";
import { NewPostButton } from "./NewPostButton";

interface Author {
  handle: string;
  displayName?: string;
  avatar?: string;
}

interface BskyPost {
  uri: string;
  author?: Author;
  record: {
    text: string;
    createdAt?: string;
  };
}

interface FeedItem {
  post: BskyPost;
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


export const Route = createFileRoute("/_requireAuth/handle/$handle")({
  async loader({ context, params }) {
    const publicItems: FeedItem[] = await getAuthorFeed(context.authManager, params.handle);
    const privateItems: PrivatePost[] = await getPrivatePosts(context.authManager, params.handle);
    const profile: Profile = await getProfile(context.authManager, params.handle);

    const entries: FeedEntry[] = [
      ...privateItems.map(({ uri, value }): FeedEntry => ({
        uri,
        text: value.text,
        createdAt: value.createdAt,
        kind: "private",
        author: {
          handle: profile.handle,
          displayName: profile.displayName,
          avatar: profile.avatar,
        },
        replyToHandle: value.reply !== undefined ? null : undefined,
      })),
      ...publicItems.map(({ post, reply, reason }): FeedEntry => ({
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
    const { handle } = Route.useParams();
    const entries = Route.useLoaderData();
    const { authManager, myProfile } = Route.useRouteContext();
    return (
      <>
        <nav>
          <ul>
            <li>
              <Link to="/">← Greensky</Link>
            </li>
            <li>
              <h3>@{handle}'s feed</h3>
            </li>
          </ul>
          <ul>
            <li>
              <span>@{myProfile?.handle}</span>
            </li>
            <li>
              <NewPostButton authManager={authManager} />
            </li>
            <li>
              <button className="secondary" onClick={authManager.logout}>Logout</button>
            </li>
          </ul>
        </nav>
        <Feed entries={entries} />
      </>
    );
  },
});


async function getAuthorFeed(
  authManager: AuthManager,
  handle: string,
): Promise<FeedItem[]> {
  const headers = new Headers();
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
