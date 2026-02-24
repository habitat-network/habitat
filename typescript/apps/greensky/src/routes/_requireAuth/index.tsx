import { createFileRoute } from "@tanstack/react-router";
import { AuthManager } from "internal/authManager.js";
import { getPrivatePosts, getProfile, PrivatePost, type Profile } from "../../habitatApi";
import { type FeedEntry, Feed } from "../../Feed";
import { NewPostButton } from "./NewPostButton";

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
    const [bskyItems, privatePosts] = await Promise.all([
      getBskyFeed(context.authManager),
      getPrivatePosts(context.authManager),
    ]);

    const privatePostToFeedEntry = async (post: PrivatePost): Promise<FeedEntry> => {
      const did = post.uri.split("/")[2];
      const privateAuthor: Profile | undefined = did
        ? await getProfile(context.authManager, did)
        : undefined;

      return {
        uri: post.uri,
        text: post.value.text,
        createdAt: post.value.createdAt,
        kind: "private",
        author: privateAuthor,
        replyToHandle: post.value.reply !== undefined ? null : undefined,
      }
    }
    const privateEntries = await Promise.all(privatePosts.map(privatePostToFeedEntry))

    const entries: FeedEntry[] = [
      ...privateEntries,
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
    const { authManager, myProfile } = Route.useRouteContext();
    const entries = Route.useLoaderData();
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

async function getBskyFeed(authManager: AuthManager): Promise<BskyFeedItem[]> {
  const headers = new Headers();
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
